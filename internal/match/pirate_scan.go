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

// in-game save-folder names that pirates / repacks commonly write to.
var inGameSaveDirNames = []string{
	"save", "saves", "savegame", "savegames",
	"userdata", "user_data", "userdat",
	"profile", "profiles",
	"sav", "savesgame",
	"appdata", // some games ship an in-folder appdata override
}

// pirateScanHubs are AppData-style roots where pirate saves typically land
// under a single game-named subfolder.
func pirateScanHubs() []string {
	home := os.Getenv("USERPROFILE")
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	low := ""
	if la := os.Getenv("LOCALAPPDATA"); la != "" {
		low = filepath.Join(filepath.Dir(la), "LocalLow")
	}
	return []string{
		os.Getenv("APPDATA"),
		os.Getenv("LOCALAPPDATA"),
		low,
		filepath.Join(home, "Saved Games"),
		filepath.Join(home, "Documents"),
		filepath.Join(home, "Documents", "My Games"),
	}
}

// pirateVendorBlacklist mirrors the AppData/Roaming vendor names we never
// treat as games (matches discover.go's defense).
var pirateVendorBlacklist = map[string]bool{
	"adobe": true, "autodesk": true, "microsoft": true, "google": true,
	"mozilla": true, "nvidia": true, "intel": true, "amd": true,
	"vmware": true, "jetbrains": true, "packages": true, "temp": true,
	"crashdumps": true, "cef": true, "easyanticheat": true,
	"comms": true, "connecteddevicesplatform": true, "d3dscache": true,
	"amdmodernui": true, "discord": true, "telegram": true,
	"anydesk": true, "parsec": true, "obsidian": true,
	"github": true, "gnupg": true, "wechat": true, "skype": true,
	"opera": true, "brave": true, "spotify": true, "notion": true,
	"whatsapp": true, "teamviewer": true, "go": true, "go-build": true,
	"java": true, "npm-cache": true, "claude": true, "termius": true,
	"termius-updater": true, "synologydrive": true, "msi": true,
	"lm-studio-updater": true, "ms-studio-updater": true,
	"snyk": true, "snyk-ls": true, "speech": true, "code": true,
	"package cache": true, "peerdistrepub": true, "fastsone": true,
	"faststone": true, "openboxlab": true, "msfree inc": true,
	"link2ea": true, "eadesktop": true, "ealauncherhelper": true,
	"protonvpn": true, "proton": true,
	"clienthelper-updater": true,
	"setup": true, "nvm": true, "disc-soft": true, "daemon tools": true,
	"flydigi": true, "pantum": true, "mintty": true,
	"toastnotificationmanagercompat": true,
	"electron": true, "chromium": true,
	"ori and the blind forest_uninstall": true,
}

// pirateScanGame searches every save hub for top-level subfolders whose name
// matches the given game's token set, plus typical save subfolders inside
// each installation root. Anything found is registered as a SaveLocation
// with source_hint="pirate".
//
// Returns the list of added paths so MatchAll can keep its `known` set fresh.
func (s *Service) pirateScanGame(g *domain.Game, known map[string]bool) []string {
	added := []string{}

	gameTokens := uniqueTokens(tokenize(g.Name))
	// Strict: 1-token names get a min-length guard so "Apex" doesn't grab
	// "ApexThemes" but "Starfield" / "Subverse" / "Roblox" still work.
	if len(gameTokens) == 0 {
		return added
	}
	if len(gameTokens) == 1 && len(gameTokens[0]) < 5 {
		// Allow 1-token only when there's also an exact normalized match
		// available, which the hub-walk loop handles via normalizeName.
	}

	gameTokKey := nameKey(gameTokens)
	gameNorm := normalizeName(g.Name)

	tryAttach := func(path string) {
		key := normalizePathKey(path)
		if known[key] {
			return
		}
		st, err := os.Stat(path)
		if err != nil {
			return
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
		if count == 0 || size > maxSaveBytes {
			return
		}
		loc := &domain.SaveLocation{
			ID:           util.SaveLocationID(g.ID, path),
			GameID:       g.ID,
			Path:         path,
			Kind:         classifySaveKind(path),
			SourceHint:   "pirate",
			SizeBytes:    size,
			FileCount:    count,
			Mtime:        mtime,
			WatchEnabled: true,
		}
		if err := s.db.UpsertSaveLocation(loc); err != nil {
			return
		}
		known[key] = true
		added = append(added, path)
	}

	// (1) Walk save hubs looking for top-level folders that match the game name.
	for _, hub := range pirateScanHubs() {
		if hub == "" || !util.IsDir(hub) {
			continue
		}
		entries, err := os.ReadDir(hub)
		if err != nil {
			continue
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
			if pirateVendorBlacklist[low] {
				continue
			}
			// Match folder against the game.
			matched := false
			if normalizeName(name) == gameNorm {
				matched = true
			} else {
				folderToks := uniqueTokens(tokenize(name))
				if len(folderToks) > 0 && nameKey(folderToks) == gameTokKey {
					matched = true
				}
				// Subset both directions, but only when the game's name has
				// at least 2 unique tokens — single common words are too risky.
				if !matched && len(gameTokens) >= 2 && len(folderToks) >= 2 {
					if setSubset(gameTokens, folderToks) || setSubset(folderToks, gameTokens) {
						matched = true
					}
				}
			}
			if !matched {
				continue
			}
			tryAttach(filepath.Join(hub, name))
		}
	}

	// (2) For each installation root, look for typical in-game save subdirs.
	insts, err := s.db.ListInstallations(g.ID)
	if err == nil {
		for _, inst := range insts {
			if inst.RootPath == "" {
				continue
			}
			for _, sub := range inGameSaveDirNames {
				p := filepath.Join(inst.RootPath, sub)
				if util.IsDir(p) {
					tryAttach(p)
				}
				// Also try one level deeper: <root>\GameData\Save etc.
				if entries, err := os.ReadDir(inst.RootPath); err == nil {
					for _, e := range entries {
						if !e.IsDir() {
							continue
						}
						p2 := filepath.Join(inst.RootPath, e.Name(), sub)
						if util.IsDir(p2) {
							tryAttach(p2)
						}
					}
				}
			}
		}
	}
	return added
}

// pirateScanOrphans walks the AppData hubs once and tries to attach unmatched
// top-level folders to:
//   1. an existing game (by exact normalized / exact token-key match against
//      the game's name tokens), or
//   2. a Ludusavi-manifest entry that has Files (creating an orphan card with
//      the canonical manifest name).
//
// Lenient subset matching is intentionally NOT used here — recall is traded
// for precision. Folders that don't match strictly (e.g. random vendor dirs)
// are silently skipped. Used as the AppData-aware sibling of discoverOrphanSaves.
func (s *Service) pirateScanOrphans(ctx context.Context, known map[string]bool) {
	games, err := s.db.ListGames()
	if err != nil {
		return
	}
	// Build {token-key → existing game id} and {normalized name → game id}.
	byTok := map[string]string{}
	byNorm := map[string]string{}
	for _, g := range games {
		tk := uniqueTokens(tokenize(g.Name))
		if len(tk) > 0 {
			byTok[nameKey(tk)] = g.ID
		}
		byNorm[normalizeName(g.Name)] = g.ID
	}

	for _, hub := range pirateScanHubs() {
		if ctx.Err() != nil {
			return
		}
		if hub == "" || !util.IsDir(hub) {
			continue
		}
		entries, err := os.ReadDir(hub)
		if err != nil {
			continue
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
			if pirateVendorBlacklist[low] {
				continue
			}
			dir := filepath.Join(hub, name)
			if known[normalizePathKey(dir)] {
				continue
			}

			// 1) Match folder name to an existing game.
			if id, ok := byNorm[normalizeName(name)]; ok {
				s.attachOrphanToGame(id, dir, "pirate", known)
				continue
			}
			toks := uniqueTokens(tokenize(name))
			if len(toks) >= 2 {
				if id, ok := byTok[nameKey(toks)]; ok {
					s.attachOrphanToGame(id, dir, "pirate", known)
					continue
				}
			}

			// 2) Strict Ludusavi-lookup (entries with Files only).
			entry, canonicalName := s.lookupByNameStrict(name)
			if entry == nil {
				continue
			}
			// Skip if the matched manifest entry's actual save paths don't
			// include this directory — guards against attaching random
			// publisher-named folders to games whose saves are elsewhere.
			vars := DefaultVars(canonicalName, "")
			ourPathKey := normalizePathKey(dir)
			referenced := false
			for pattern := range entry.Files {
				rendered := Render(pattern, vars)
				for _, p := range expandGlobs(rendered) {
					rendKey := normalizePathKey(p)
					if rendKey == ourPathKey ||
						strings.HasPrefix(rendKey, ourPathKey+string(filepath.Separator)) ||
						strings.HasPrefix(ourPathKey, rendKey+string(filepath.Separator)) {
						referenced = true
						break
					}
				}
				if referenced {
					break
				}
			}
			if !referenced {
				continue
			}
			// Create stub orphan game pointing at this manifest entry.
			g, err := s.db.FindGameBySlug(util.Slug(canonicalName))
			if err == sqlite.ErrNotFound {
				g = &domain.Game{
					ID:   util.GameID(util.Slug(canonicalName), canonicalName),
					Name: canonicalName,
					Slug: util.Slug(canonicalName),
				}
				if entry.Steam != nil {
					g.SteamAppID = entry.Steam.ID
				}
				if err := s.db.UpsertGame(g); err != nil {
					continue
				}
				byTok[nameKey(uniqueTokens(tokenize(canonicalName)))] = g.ID
				byNorm[normalizeName(canonicalName)] = g.ID
			} else if err != nil {
				continue
			}
			s.attachOrphanToGame(g.ID, dir, "pirate", known)
		}
	}
}

// attachOrphanToGame upserts dir as a SaveLocation under gameID with the
// given sourceHint, updates known.
func (s *Service) attachOrphanToGame(gameID, dir, src string, known map[string]bool) {
	st, err := os.Stat(dir)
	if err != nil || !st.IsDir() {
		return
	}
	size, count, mtime := util.DirSizeAndCount(dir)
	if count == 0 || size > maxSaveBytes {
		return
	}
	loc := &domain.SaveLocation{
		ID:           util.SaveLocationID(gameID, dir),
		GameID:       gameID,
		Path:         dir,
		Kind:         classifySaveKind(dir),
		SourceHint:   src,
		SizeBytes:    size,
		FileCount:    count,
		Mtime:        mtime,
		WatchEnabled: true,
	}
	if err := s.db.UpsertSaveLocation(loc); err != nil {
		slog.Warn("attach orphan", "err", err)
		return
	}
	known[normalizePathKey(dir)] = true
}
