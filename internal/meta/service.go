package meta

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"GameSaver/internal/config"
	"GameSaver/internal/domain"
	"GameSaver/internal/storage/sqlite"
)

// Service downloads + caches covers/heroes/icons for games and stores their paths.
type Service struct {
	cfg    *config.Config
	db     *sqlite.Store
	client *http.Client
}

func New(cfg *config.Config, db *sqlite.Store) *Service {
	return &Service{
		cfg: cfg,
		db:  db,
		client: &http.Client{
			Timeout: 20 * time.Second,
		},
	}
}

// EnrichGame fetches cover/hero/icon for a single game.
func (s *Service) EnrichGame(ctx context.Context, g *domain.Game) error {
	cover, hero, icon := s.fetch(ctx, g)
	if cover == "" && hero == "" && icon == "" {
		return nil
	}
	return s.db.UpdateGameCovers(g.ID, cover, hero, icon)
}

// EnrichAll iterates all games and tries to add cover/hero/icon paths.
// Best-effort; runs concurrently per game with a small worker pool.
func (s *Service) EnrichAll(ctx context.Context, emit func(string, any)) {
	games, err := s.db.ListGames()
	if err != nil {
		slog.Warn("list games for enrich", "err", err)
		return
	}
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	for _, g := range games {
		if ctx.Err() != nil {
			return
		}
		if g.CoverPath != "" && g.HeroPath != "" {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(g *domain.Game) {
			defer wg.Done()
			defer func() { <-sem }()
			cover, hero, icon := s.fetch(ctx, g)
			if cover == "" && hero == "" && icon == "" {
				return
			}
			if err := s.db.UpdateGameCovers(g.ID, cover, hero, icon); err != nil {
				slog.Warn("update covers", "id", g.ID, "err", err)
				return
			}
			if emit != nil {
				emit("meta:game", map[string]any{"id": g.ID})
			}
		}(g)
	}
	wg.Wait()
}

func (s *Service) fetch(ctx context.Context, g *domain.Game) (cover, hero, icon string) {
	// Priority 1: Steam CDN (no key, very reliable for Steam games).
	if g.SteamAppID > 0 {
		cover = s.tryDownload(ctx, g, "cover", fmt.Sprintf("https://cdn.cloudflare.steamstatic.com/steam/apps/%d/library_600x900_2x.jpg", g.SteamAppID))
		if cover == "" {
			cover = s.tryDownload(ctx, g, "cover", fmt.Sprintf("https://cdn.cloudflare.steamstatic.com/steam/apps/%d/library_600x900.jpg", g.SteamAppID))
		}
		hero = s.tryDownload(ctx, g, "hero", fmt.Sprintf("https://cdn.cloudflare.steamstatic.com/steam/apps/%d/library_hero.jpg", g.SteamAppID))
		if hero == "" {
			hero = s.tryDownload(ctx, g, "hero", fmt.Sprintf("https://cdn.cloudflare.steamstatic.com/steam/apps/%d/header.jpg", g.SteamAppID))
		}
		icon = s.tryDownload(ctx, g, "icon", fmt.Sprintf("https://cdn.cloudflare.steamstatic.com/steam/apps/%d/capsule_184x69.jpg", g.SteamAppID))
	}

	// Priority 2: SteamGridDB by AppID (if Steam) — gives nicer artwork variants.
	if s.cfg.SteamGridDBKey != "" {
		if g.SteamAppID > 0 {
			if cover == "" {
				if u := s.sgdbFirstAsset(ctx, "grids/steam", g.SteamAppID, "?dimensions=600x900&types=static"); u != "" {
					cover = s.tryDownload(ctx, g, "cover", u)
				}
			}
			if hero == "" {
				if u := s.sgdbFirstAsset(ctx, "heroes/steam", g.SteamAppID, "?types=static"); u != "" {
					hero = s.tryDownload(ctx, g, "hero", u)
				}
			}
			if icon == "" {
				if u := s.sgdbFirstAsset(ctx, "icons/steam", g.SteamAppID, ""); u != "" {
					icon = s.tryDownload(ctx, g, "icon", u)
				}
			}
		} else {
			// Search by name -> game id -> assets
			if id := s.sgdbSearch(ctx, g.Name); id > 0 {
				if cover == "" {
					if u := s.sgdbFirstAsset(ctx, "grids/game", id, "?dimensions=600x900&types=static"); u != "" {
						cover = s.tryDownload(ctx, g, "cover", u)
					}
				}
				if hero == "" {
					if u := s.sgdbFirstAsset(ctx, "heroes/game", id, "?types=static"); u != "" {
						hero = s.tryDownload(ctx, g, "hero", u)
					}
				}
				if icon == "" {
					if u := s.sgdbFirstAsset(ctx, "icons/game", id, ""); u != "" {
						icon = s.tryDownload(ctx, g, "icon", u)
					}
				}
			}
		}
	}
	return
}

type sgdbAsset struct {
	URL       string `json:"url"`
	Thumb     string `json:"thumb"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	MIME      string `json:"mime"`
}

type sgdbList struct {
	Success bool         `json:"success"`
	Data    []sgdbAsset  `json:"data"`
	Errors  []string     `json:"errors"`
}

type sgdbSearchResult struct {
	Success bool `json:"success"`
	Data    []struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	} `json:"data"`
}

// sgdbFirstAsset hits SteamGridDB and returns the first asset URL.
func (s *Service) sgdbFirstAsset(ctx context.Context, kind string, id int64, query string) string {
	endpoint := fmt.Sprintf("https://www.steamgriddb.com/api/v2/%s/%d%s", kind, id, query)
	req, _ := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	req.Header.Set("Authorization", "Bearer "+s.cfg.SteamGridDBKey)
	resp, err := s.client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return ""
	}
	var list sgdbList
	if err := decodeJSON(resp.Body, &list); err != nil || !list.Success || len(list.Data) == 0 {
		return ""
	}
	return list.Data[0].URL
}

// sgdbSearch returns the first matching SteamGridDB game id, or 0.
func (s *Service) sgdbSearch(ctx context.Context, name string) int64 {
	endpoint := fmt.Sprintf("https://www.steamgriddb.com/api/v2/search/autocomplete/%s", url.PathEscape(name))
	req, _ := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	req.Header.Set("Authorization", "Bearer "+s.cfg.SteamGridDBKey)
	resp, err := s.client.Do(req)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return 0
	}
	var sr sgdbSearchResult
	if err := decodeJSON(resp.Body, &sr); err != nil || !sr.Success || len(sr.Data) == 0 {
		return 0
	}
	return sr.Data[0].ID
}

// tryDownload downloads URL and stores it under covers/<gameID>_<kind>.<ext>. Returns local path or "".
func (s *Service) tryDownload(ctx context.Context, g *domain.Game, kind, urlStr string) string {
	req, err := http.NewRequestWithContext(ctx, "GET", urlStr, nil)
	if err != nil {
		return ""
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return ""
	}
	ext := guessExt(resp.Header.Get("Content-Type"), urlStr)
	if ext == "" {
		ext = ".jpg"
	}
	// Filename uses hash of URL to be content-keyed (caching across runs).
	h := sha1.Sum([]byte(g.ID + "|" + kind + "|" + urlStr))
	fn := fmt.Sprintf("%s_%s_%s%s", g.ID, kind, hex.EncodeToString(h[:6]), ext)
	full := filepath.Join(s.cfg.CoversDir(), fn)
	if _, err := os.Stat(full); err == nil {
		return fn
	}
	tmp := full + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return ""
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		os.Remove(tmp)
		return ""
	}
	f.Close()
	if err := os.Rename(tmp, full); err != nil {
		return ""
	}
	return fn
}

func guessExt(ct, urlStr string) string {
	ct = strings.ToLower(ct)
	switch {
	case strings.Contains(ct, "png"):
		return ".png"
	case strings.Contains(ct, "webp"):
		return ".webp"
	case strings.Contains(ct, "jpeg"), strings.Contains(ct, "jpg"):
		return ".jpg"
	case strings.Contains(ct, "gif"):
		return ".gif"
	}
	low := strings.ToLower(urlStr)
	for _, ext := range []string{".jpg", ".jpeg", ".png", ".webp", ".gif"} {
		if strings.HasSuffix(strings.Split(low, "?")[0], ext) {
			if ext == ".jpeg" {
				return ".jpg"
			}
			return ext
		}
	}
	return ""
}
