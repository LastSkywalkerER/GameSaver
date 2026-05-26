package match

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"GameSaver/internal/domain"
	"GameSaver/internal/storage/sqlite"
	"GameSaver/internal/util"
)

// ReverseScanResult is the summary returned to the UI when ReverseScan finishes.
type ReverseScanResult struct {
	EntriesChecked    int   `json:"entriesChecked"`
	EntriesMatched    int   `json:"entriesMatched"`
	NewGamesCreated   int   `json:"newGamesCreated"`
	NewLocationsAdded int   `json:"newLocationsAdded"`
	DurationMS        int64 `json:"durationMs"`
}

// ReverseScan walks every Ludusavi manifest entry, expands its file patterns
// with the current user's variables, and for any pattern that resolves to an
// existing on-disk path it creates (or augments) a Game card. This is the
// "find any save I've ever made" hammer — slow (~30s on SSD over 53000 entries)
// but exhaustive. Entries whose patterns reference <base> are skipped because
// we don't know the install dir for not-installed games.
//
// emit is called with "reverse:progress" {checked, matched} every ~200 entries.
func (s *Service) ReverseScan(ctx context.Context, emit func(string, any)) (*ReverseScanResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.buildIndex()
	start := time.Now()
	res := &ReverseScanResult{}

	// Pre-load existing games' save paths and Steam AppIDs so we don't duplicate.
	games, err := s.db.ListGames()
	if err != nil {
		return nil, err
	}
	knownPaths := map[string]bool{}
	knownSteamAppID := map[int64]string{}
	for _, g := range games {
		if g.SteamAppID > 0 {
			knownSteamAppID[g.SteamAppID] = g.ID
		}
		locs, _ := s.db.ListSaveLocations(g.ID)
		for _, l := range locs {
			knownPaths[normalizePathKey(l.Path)] = true
		}
	}

	for key, entry := range s.manifest {
		if ctx.Err() != nil {
			break
		}
		// Skip alias entries — their canonical sibling carries the same files
		// and is the right cardinal name. Walking both would create duplicate
		// games (e.g. "Alan Wake 2" + "Alan Wake II" both pointing at the same
		// AppData folder).
		if _, isAlias := s.aliasOf[key]; isAlias {
			continue
		}
		res.EntriesChecked++
		if emit != nil && res.EntriesChecked%500 == 0 {
			emit("reverse:progress", map[string]any{
				"checked": res.EntriesChecked,
				"matched": res.EntriesMatched,
				"created": res.NewGamesCreated,
			})
		}
		if len(entry.Files) == 0 {
			continue
		}
		vars := DefaultVars(key, "")

		// First pass: gather existing paths for this entry.
		hits := []pathCand{}
		for pattern := range entry.Files {
			// Patterns that reference the install root require an install dir
			// we don't have for orphan games — skip them here.
			if strings.Contains(pattern, "<base>") || strings.Contains(pattern, "<root>") {
				continue
			}
			// Also skip mac/linux-only paths (rendered placeholders point at /...).
			if strings.HasPrefix(pattern, "/") || strings.HasPrefix(pattern, "~/") {
				continue
			}
			rendered := Render(pattern, vars)
			isGlob := strings.ContainsAny(pattern, "*?[") || strings.ContainsAny(rendered, "*?")
			for _, p := range expandGlobs(rendered) {
				if !util.PathExists(p) {
					continue
				}
				st, err := osStat(p)
				if err != nil {
					continue
				}
				// File-glob coalesce: same parent rule as applyManifestEntry.
				if isGlob && !st.IsDir() {
					hits = append(hits, pathCand{path: p, isDir: false})
				} else {
					hits = append(hits, pathCand{path: p, isDir: st.IsDir()})
				}
			}
		}
		if len(hits) == 0 {
			continue
		}

		// Pre-filter: if every hit is already attached to another game,
		// skip this entry entirely — no point creating an empty stub.
		anyNew := false
		for _, c := range hits {
			if !knownPaths[normalizePathKey(c.path)] {
				anyNew = true
				break
			}
		}
		if !anyNew {
			continue
		}
		res.EntriesMatched++

		// Find or create the game. Race-safe: re-query DB right before
		// creating a new stub so a recent matchAll that set AppIDs on existing
		// games is still seen.
		var g *domain.Game
		if entry.Steam != nil && entry.Steam.ID > 0 {
			if id, ok := knownSteamAppID[entry.Steam.ID]; ok {
				g, _ = s.db.GetGame(id)
			}
			if g == nil {
				if existing, err := s.db.FindGameBySteamAppID(entry.Steam.ID); err == nil && existing != nil {
					g = existing
					knownSteamAppID[entry.Steam.ID] = g.ID
				}
			}
		}
		if g == nil {
			if existing, err := s.db.FindGameBySlug(util.Slug(key)); err == nil && existing != nil {
				g = existing
			}
		}
		if g == nil {
			g = &domain.Game{
				ID:   util.GameID(util.Slug(key), key),
				Name: key,
				Slug: util.Slug(key),
			}
			if entry.Steam != nil {
				g.SteamAppID = entry.Steam.ID
			}
			if err := s.db.UpsertGame(g); err != nil {
				slog.Warn("reverse: create game", "name", key, "err", err)
				continue
			}
			res.NewGamesCreated++
			if entry.Steam != nil && entry.Steam.ID > 0 {
				knownSteamAppID[entry.Steam.ID] = g.ID
			}
		}

		// Coalesce hits with the same dedup pipeline applyManifestEntry uses.
		hits = dropDescendants(hits)
		hits = coalesceByNamespace(hits)
		hits = dropDescendants(hits)

		for _, c := range hits {
			pathKey := normalizePathKey(c.path)
			if knownPaths[pathKey] {
				continue
			}
			var size, mtime int64
			var count int
			st, err := osStat(c.path)
			if err != nil {
				continue
			}
			if st.IsDir() {
				size, count, mtime = util.DirSizeAndCount(c.path)
			} else {
				size = st.Size()
				count = 1
				mtime = st.ModTime().Unix()
			}
			if count == 0 || size > maxSaveBytes {
				continue
			}
			loc := &domain.SaveLocation{
				ID:           util.SaveLocationID(g.ID, c.path),
				GameID:       g.ID,
				Path:         c.path,
				Kind:         classifySaveKind(c.path),
				SourceHint:   "reverse",
				SizeBytes:    size,
				FileCount:    count,
				Mtime:        mtime,
				WatchEnabled: true,
			}
			if err := s.db.UpsertSaveLocation(loc); err != nil {
				continue
			}
			knownPaths[pathKey] = true
			res.NewLocationsAdded++
		}
	}

	// Final per-game dedup + AppID merge, identical to MatchAll's last steps.
	s.dedupSaveLocationsPerGame()
	s.mergeDuplicatesBySteamAppID()

	res.DurationMS = time.Since(start).Milliseconds()
	if emit != nil {
		emit("reverse:done", res)
	}
	_ = sqlite.ErrNotFound // keep import even if branches diverge
	return res, nil
}
