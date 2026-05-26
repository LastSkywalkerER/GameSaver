package match

import (
	"context"
	"log/slog"
	"sort"
	"strings"

	"GameSaver/internal/domain"
	"GameSaver/internal/util"
)

// ManifestSearchResult is the per-entry shape exposed to the UI for the
// "re-pick game" dialog.
type ManifestSearchResult struct {
	Key        string `json:"key"`
	Name       string `json:"name"`
	SteamAppID int64  `json:"steamAppId,omitempty"`
	FileCount  int    `json:"fileCount"`
}

// SearchManifest does a token-aware fuzzy lookup over manifest entry names.
// Empty query returns the first `limit` entries alphabetically. Limit ≤0
// defaults to 30.
func (s *Service) SearchManifest(query string, limit int) []ManifestSearchResult {
	s.buildIndex()
	if limit <= 0 || limit > 300 {
		limit = 30
	}
	q := strings.TrimSpace(query)
	qTokens := tokenize(q)
	results := make([]ManifestSearchResult, 0)
	type scored struct {
		r     ManifestSearchResult
		score int
	}
	bucket := make([]scored, 0, 200)
	for k, v := range s.manifest {
		score := 0
		if q == "" {
			score = 1
		} else {
			lowK := strings.ToLower(k)
			lowQ := strings.ToLower(q)
			if lowK == lowQ {
				score = 1000
			} else if strings.Contains(lowK, lowQ) {
				score = 200 + (50 - min(50, len(k)))
			}
			if len(qTokens) > 0 {
				mt := tokenize(k)
				common := setIntersectCount(qTokens, mt)
				score += common * 30
			}
		}
		if score == 0 {
			continue
		}
		var sid int64
		if v.Steam != nil {
			sid = v.Steam.ID
		}
		bucket = append(bucket, scored{
			r: ManifestSearchResult{
				Key:        k,
				Name:       k,
				SteamAppID: sid,
				FileCount:  len(v.Files),
			},
			score: score,
		})
	}
	sort.Slice(bucket, func(i, j int) bool {
		if bucket[i].score != bucket[j].score {
			return bucket[i].score > bucket[j].score
		}
		return bucket[i].r.Name < bucket[j].r.Name
	})
	if len(bucket) > limit {
		bucket = bucket[:limit]
	}
	for _, b := range bucket {
		results = append(results, b.r)
	}
	return results
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func setIntersectCount(a, b []string) int {
	bs := map[string]bool{}
	for _, x := range b {
		bs[x] = true
	}
	n := 0
	seen := map[string]bool{}
	for _, x := range a {
		if seen[x] {
			continue
		}
		seen[x] = true
		if bs[x] {
			n++
		}
	}
	return n
}

// RematchGame pins the game to the given manifest key (or clears the pin if
// manifestKey == ""), wipes existing auto-discovered save locations for that
// game, and re-runs the match against the chosen entry. Manually-added save
// locations (source_hint == "manual") are preserved.
func (s *Service) RematchGame(ctx context.Context, gameID, manifestKey string) error {
	s.buildIndex()
	g, err := s.db.GetGame(gameID)
	if err != nil {
		return err
	}
	if err := s.db.SetManifestOverride(gameID, manifestKey); err != nil {
		return err
	}
	// Optionally update the display name to the manifest's canonical name.
	if manifestKey != "" && manifestKey != g.Name {
		g.Name = manifestKey
		g.Slug = util.Slug(manifestKey)
		if _, ok := s.manifest[manifestKey]; ok {
			if e := s.manifest[manifestKey]; e.Steam != nil && e.Steam.ID > 0 {
				g.SteamAppID = e.Steam.ID
				// Strip the wrong cover so meta enrichment picks up the right one.
				g.CoverPath = ""
				g.HeroPath = ""
				g.IconPath = ""
			}
		}
		if err := s.db.UpsertGame(g); err != nil {
			return err
		}
	}
	// Wipe non-manual save locations for this game so they get rebuilt cleanly.
	if locs, err := s.db.ListSaveLocations(gameID); err == nil {
		for _, l := range locs {
			if l.SourceHint != "manual" {
				_ = s.db.DeleteSaveLocation(l.ID)
			}
		}
	}
	if _, err := s.matchGame(g); err != nil {
		return err
	}
	return nil
}

// AddManualSaveLocation registers a user-supplied save folder under the game.
// Returns the new SaveLocation row.
func (s *Service) AddManualSaveLocation(gameID, path string) (*domain.SaveLocation, error) {
	g, err := s.db.GetGame(gameID)
	if err != nil {
		return nil, err
	}
	st, err := osStat(path)
	if err != nil {
		return nil, err
	}
	var size, mtime int64
	var count int
	if st.IsDir() {
		size, count, mtime = util.DirSizeAndCount(path)
	} else {
		size = st.Size()
		count = 1
		mtime = st.ModTime().Unix()
	}
	loc := &domain.SaveLocation{
		ID:           util.SaveLocationID(g.ID, path),
		GameID:       g.ID,
		Path:         path,
		Kind:         classifySaveKind(path),
		SourceHint:   "manual",
		SizeBytes:    size,
		FileCount:    count,
		Mtime:        mtime,
		WatchEnabled: true,
	}
	if err := s.db.UpsertSaveLocation(loc); err != nil {
		return nil, err
	}
	slog.Info("manual save location added", "game", g.Name, "path", path)
	return loc, nil
}

// RemoveSaveLocation deletes a single save location.
func (s *Service) RemoveSaveLocation(id string) error {
	return s.db.DeleteSaveLocation(id)
}

// DeepScanResult is one candidate folder surfaced by DeepScanGame.
type DeepScanResult struct {
	Path      string `json:"path"`
	FileCount int    `json:"fileCount"`
	SizeBytes int64  `json:"sizeBytes"`
	Mtime     int64  `json:"mtime"`
	Score     int    `json:"score"`
	Reason    string `json:"reason"`
}

// DeepScanGame walks Saved Games, Documents\My Games, AppData (Local/Roaming/Low)
// looking for folders whose name shares tokens with the game's name, ranked by
// token overlap. Surfaces them as candidates the user can accept.
func (s *Service) DeepScanGame(gameID string) ([]*DeepScanResult, error) {
	s.buildIndex()
	g, err := s.db.GetGame(gameID)
	if err != nil {
		return nil, err
	}
	gameTokens := uniqueTokens(tokenize(g.Name))
	if len(gameTokens) == 0 {
		return nil, nil
	}
	// Build search roots.
	home := osGetenv("USERPROFILE")
	appData := osGetenv("APPDATA")
	localApp := osGetenv("LOCALAPPDATA")
	low := ""
	if localApp != "" {
		low = strings.Replace(localApp, "Local", "LocalLow", 1)
	}
	roots := []string{
		joinIfPresent(home, "Saved Games"),
		joinIfPresent(home, "Documents", "My Games"),
		joinIfPresent(home, "Documents"),
		appData, localApp, low,
	}
	// Track existing save paths to skip.
	existing := map[string]bool{}
	if locs, _ := s.db.ListSaveLocations(gameID); locs != nil {
		for _, l := range locs {
			existing[normalizePathKey(l.Path)] = true
		}
	}
	out := []*DeepScanResult{}
	for _, r := range roots {
		if r == "" {
			continue
		}
		walkDeepScan(r, gameTokens, existing, &out, 0, 3)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].FileCount > out[j].FileCount
	})
	if len(out) > 50 {
		out = out[:50]
	}
	return out, nil
}
