package match

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"GameSaver/internal/domain"
	"GameSaver/internal/storage/sqlite"
	"GameSaver/internal/util"
)

// discoverOrphanSaves walks the two conventional save hubs whose subfolders are
// almost always game-named ("Saved Games\*" and "Documents\My Games\*"), and for
// each unattached subfolder tries to find a matching Ludusavi entry under STRICT
// rules. AppData is intentionally NOT walked here: vendor sprawl (NVIDIA, Adobe,
// JetBrains, …) makes fuzzy matching unreliable. AppData saves for installed
// games are already covered by the per-game manifest pass.
func (s *Service) discoverOrphanSaves(ctx context.Context, knownPaths map[string]bool) {
	if len(s.byTokenKey) == 0 {
		s.buildIndex()
	}
	home := os.Getenv("USERPROFILE")
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	hubs := []string{
		filepath.Join(home, "Saved Games"),
		filepath.Join(home, "Documents", "My Games"),
	}
	for _, hub := range hubs {
		if ctx.Err() != nil {
			return
		}
		s.discoverHub(hub, knownPaths, 1)
	}
}

func (s *Service) discoverHub(hub string, knownPaths map[string]bool, depth int) {
	if depth < 0 || !util.IsDir(hub) {
		return
	}
	entries, err := os.ReadDir(hub)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		low := strings.ToLower(name)
		if strings.HasPrefix(low, ".") || strings.HasPrefix(low, "$") {
			continue
		}
		dir := filepath.Join(hub, name)
		if knownPaths[normalizePathKey(dir)] {
			continue
		}
		entry, gameName := s.lookupByNameStrict(name)
		if entry == nil {
			// One step deeper: sometimes "Saved Games\<publisher>\<game>".
			if depth > 0 {
				s.discoverHub(dir, knownPaths, depth-1)
			}
			continue
		}
		size, count, mtime := util.DirSizeAndCount(dir)
		if count == 0 || size > maxSaveBytes {
			continue
		}
		// Prefer reusing an existing game with the same Steam AppID — avoids
		// creating both "Crysis2Remastered" (installed folder) and "Crysis 2
		// Remastered" (manifest name) as separate entries.
		var g *domain.Game
		if entry.Steam != nil && entry.Steam.ID > 0 {
			if existing, err := s.db.FindGameBySteamAppID(entry.Steam.ID); err == nil && existing != nil {
				g = existing
			}
		}
		if g == nil {
			existing, err := s.db.FindGameBySlug(util.Slug(gameName))
			if err == nil {
				g = existing
			} else if err == sqlite.ErrNotFound {
				g = &domain.Game{
					ID:   util.GameID(util.Slug(gameName), gameName),
					Name: gameName,
					Slug: util.Slug(gameName),
				}
				if entry.Steam != nil {
					g.SteamAppID = entry.Steam.ID
				}
				if err := s.db.UpsertGame(g); err != nil {
					slog.Warn("create orphan game", "name", gameName, "err", err)
					continue
				}
			} else {
				continue
			}
		}
		loc := &domain.SaveLocation{
			ID:           util.SaveLocationID(g.ID, dir),
			GameID:       g.ID,
			Path:         dir,
			Kind:         classifySaveKind(dir),
			SourceHint:   "orphan",
			SizeBytes:    size,
			FileCount:    count,
			Mtime:        mtime,
			WatchEnabled: true,
		}
		_ = s.db.UpsertSaveLocation(loc)
	}
}

// lookupByNameStrict resolves a save-folder name to a manifest entry using only
// exact normalized / exact token-key matches. Entries with no Files are
// rejected — they're just identity stubs (Steam ID + installDir) that provide
// no evidence the discovered folder belongs to that game (avoids attributing
// Apex Legends's "Saved Games\Respawn\Apex" save to the indie "Apex").
// Also follows aliases to the canonical entry.
func (s *Service) lookupByNameStrict(folderName string) (*ManifestEntry, string) {
	pick := func(k string) (*ManifestEntry, string) {
		ck := s.canonicalKey(k)
		e, ok := s.manifest[ck]
		if !ok || len(e.Files) == 0 {
			return nil, ""
		}
		return &e, ck
	}
	if k, ok := s.byNormalized[normalizeName(folderName)]; ok {
		if e, ck := pick(k); e != nil {
			return e, ck
		}
	}
	tokens := tokenize(folderName)
	if len(tokens) < 2 {
		return nil, ""
	}
	if k, ok := s.byTokenKey[nameKey(tokens)]; ok {
		if e, ck := pick(k); e != nil {
			return e, ck
		}
	}
	return nil, ""
}
