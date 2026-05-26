package match

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"GameSaver/internal/domain"
	"GameSaver/internal/scan/pirate"
	"GameSaver/internal/storage/sqlite"
	"GameSaver/internal/util"
)

// osStat is a thin alias so the function reads cleanly above.
var osStat = os.Stat

// maxSaveBytes is a safety threshold: any "save location" larger than this is
// almost certainly a glob that escaped into the game install directory.
const maxSaveBytes = 2 * 1024 * 1024 * 1024 // 2 GB

// Service performs save-location matching against the bundled Ludusavi manifest
// + Goldberg/FLT routing for piracy variants + orphan-save discovery.
type Service struct {
	db   *sqlite.Store
	once sync.Once
	// mu serializes MatchAll, ReverseScan, RematchGame and the like so two
	// concurrent passes (e.g. a Scan still finishing its match phase when the
	// user clicks "Поиск сейвов") can't race on knownSteamAppID and produce
	// duplicate stub games.
	mu sync.Mutex

	// raw manifest entries by their original key (for path expansion).
	manifest map[string]ManifestEntry
	// inverted indices for fast lookup.
	byNormalized map[string]string // normalized name -> manifest key
	byTokenKey   map[string]string // sorted-token key -> manifest key
	bySteamID    map[int64]string  // steam AppID -> manifest key
	// indexByToken is used by discover.go to walk manifest tokens.
	indexByToken map[string]string
	// aliasOf[k] is the canonical manifest key when k is an alias
	// (e.g. "Alan Wake 2" → "Alan Wake II").
	aliasOf map[string]string
}

// canonicalKey returns the canonical manifest key for k (following alias
// chains), or k itself if it isn't an alias.
func (s *Service) canonicalKey(k string) string {
	for hops := 0; hops < 4; hops++ {
		alias, ok := s.aliasOf[k]
		if !ok || alias == "" || alias == k {
			return k
		}
		k = alias
	}
	return k
}

func New(db *sqlite.Store) *Service { return &Service{db: db} }

// MatchAll matches every Game currently in the DB.
func (s *Service) MatchAll(ctx context.Context, emit func(string, any)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.once.Do(s.loadManifest)

	games, err := s.db.ListGames()
	if err != nil {
		return err
	}

	// Track already-attached save paths so orphan discovery skips them.
	// We also seed it with every save location already in DB (from a prior scan),
	// so re-runs don't re-create stub games for paths that match an installed game.
	known := map[string]bool{}
	for _, g := range games {
		locs, _ := s.db.ListSaveLocations(g.ID)
		for _, l := range locs {
			known[normalizePathKey(l.Path)] = true
		}
	}

	for _, g := range games {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		paths, err := s.matchGame(g)
		if err != nil {
			slog.Warn("match game", "name", g.Name, "err", err)
		}
		for _, p := range paths {
			known[normalizePathKey(p)] = true
		}
		// Per-game "pirate scan": look in AppData / Saved Games / Documents
		// for folders matching this game's name, plus typical in-game
		// save subdirs (save/saves/savegames/profile/...). Catches pirate
		// builds that drop saves into non-Ludusavi paths.
		for _, p := range s.pirateScanGame(g, known) {
			_ = p
		}
		if emit != nil {
			emit("match:game", map[string]any{"id": g.ID, "name": g.Name})
		}
	}

	// Goldberg saves by AppID.
	for _, gs := range pirate.ListGoldbergSaves() {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		g, err := s.db.FindGameBySteamAppID(gs.AppID)
		if err == sqlite.ErrNotFound {
			// Try harder: resolve the Steam display name, then look up an
			// existing game by that slug before creating a stub. This avoids
			// creating "Portal with RTX (Goldberg)" alongside the heuristic-
			// found "Portal with RTX" install — they should be one card.
			name := s.resolveSteamName(ctx, gs.AppID)
			if name == "" {
				name = fmt.Sprintf("Steam AppID %d", gs.AppID)
			}
			slug := util.Slug(name)
			if existing, errSlug := s.db.FindGameBySlug(slug); errSlug == nil && existing != nil {
				g = existing
				if g.SteamAppID == 0 {
					g.SteamAppID = gs.AppID
					_ = s.db.UpsertGame(g)
				}
			} else {
				g = &domain.Game{
					ID:         util.GameID(slug, fmt.Sprintf("steam:%d", gs.AppID)),
					Name:       name,
					Slug:       slug,
					SteamAppID: gs.AppID,
				}
				if err := s.db.UpsertGame(g); err != nil {
					slog.Warn("create goldberg stub", "appid", gs.AppID, "err", err)
					continue
				}
				if emit != nil {
					emit("match:game", map[string]any{"id": g.ID, "name": g.Name})
				}
			}
		} else if err != nil {
			slog.Warn("find game by appid", "appid", gs.AppID, "err", err)
			continue
		}
		size, count, mtime := util.DirSizeAndCount(gs.Path)
		loc := &domain.SaveLocation{
			ID:           util.SaveLocationID(g.ID, gs.Path),
			GameID:       g.ID,
			Path:         gs.Path,
			Kind:         domain.SaveKindGoldberg,
			SourceHint:   "goldberg",
			SizeBytes:    size,
			FileCount:    count,
			Mtime:        mtime,
			WatchEnabled: true,
		}
		_ = s.db.UpsertSaveLocation(loc)
		known[normalizePathKey(gs.Path)] = true
	}

	// Orphan save discovery (Saved Games, My Games).
	s.discoverOrphanSaves(ctx, known)
	// Pirate-style orphans across AppData hubs: strict name matching against
	// already-known games + Ludusavi entries whose own files reference the
	// found folder. Catches pirate save dirs like AppData\Local\Fallout4
	// that the Ludusavi manifest doesn't have, by attaching them to the
	// already-known "Fallout 4" card.
	s.pirateScanOrphans(ctx, known)
	// Final global dedup: across all games, drop SaveLocations whose path lies
	// under another SaveLocation of the same game (avoids parent + child rows
	// from match + orphan-walker running in sequence).
	s.dedupSaveLocationsPerGame()
	// Merge games that share the same Steam AppID (came from heuristic +
	// reverse-scan racing or earlier-version DBs).
	s.mergeDuplicatesBySteamAppID()
	return nil
}

// mergeDuplicatesBySteamAppID finds groups of games with the same non-zero
// Steam AppID and merges them: the "winner" (one with installations, else the
// one with the canonical-looking name) absorbs all save_locations from the
// losers, then losers are deleted. Snapshots are reassigned to the winner.
func (s *Service) mergeDuplicatesBySteamAppID() {
	games, err := s.db.ListGames()
	if err != nil {
		return
	}
	byAppID := map[int64][]*domain.Game{}
	for _, g := range games {
		if g.SteamAppID > 0 {
			byAppID[g.SteamAppID] = append(byAppID[g.SteamAppID], g)
		}
	}
	for appID, group := range byAppID {
		if len(group) < 2 {
			continue
		}
		// Pick the winner: prefer the one with installations, then the one
		// whose name has a space (looks canonical) over no-space folder-name,
		// then the longest name.
		winner := group[0]
		winnerInsts, _ := s.db.ListInstallations(winner.ID)
		for _, g := range group[1:] {
			gInsts, _ := s.db.ListInstallations(g.ID)
			better := false
			switch {
			case len(gInsts) > len(winnerInsts):
				better = true
			case len(gInsts) == len(winnerInsts) && strings.Contains(g.Name, " ") && !strings.Contains(winner.Name, " "):
				better = true
			case len(gInsts) == len(winnerInsts) && len(g.Name) > len(winner.Name):
				better = true
			}
			if better {
				winner = g
				winnerInsts = gInsts
			}
		}
		slog.Info("merging duplicate games by Steam AppID",
			"appid", appID, "winner", winner.Name, "winnerID", winner.ID, "duplicates", len(group)-1)
		for _, g := range group {
			if g.ID == winner.ID {
				continue
			}
			// Re-parent every save_location of the loser onto the winner.
			locs, _ := s.db.ListSaveLocations(g.ID)
			for _, l := range locs {
				newLoc := *l
				newLoc.GameID = winner.ID
				newLoc.ID = util.SaveLocationID(winner.ID, l.Path)
				_ = s.db.UpsertSaveLocation(&newLoc)
			}
			// Reassign snapshots (ON DELETE CASCADE would otherwise drop them).
			_, _ = s.db.DB.Exec(`UPDATE snapshots SET game_id=? WHERE game_id=?`, winner.ID, g.ID)
			// Reassign installations (rare, but possible — Goldberg game vs
			// heuristic install could both carry an installation row).
			_, _ = s.db.DB.Exec(`UPDATE installations SET game_id=? WHERE game_id=?`, winner.ID, g.ID)
			// Finally drop the loser game; its save_locations cascade-delete
			// (the re-parented copies already live under the winner).
			_, _ = s.db.DB.Exec(`DELETE FROM games WHERE id=?`, g.ID)
		}
		// Re-run per-game dedup on winner since we may now have parent+child
		// rows from the merge.
		winLocs, _ := s.db.ListSaveLocations(winner.ID)
		if len(winLocs) >= 2 {
			cands := make([]pathCand, len(winLocs))
			for i, l := range winLocs {
				cands[i] = pathCand{path: l.Path, isDir: true}
			}
			kept := dropDescendants(cands)
			keepKeys := map[string]bool{}
			for _, k := range kept {
				keepKeys[normalizePathKey(k.path)] = true
			}
			for _, l := range winLocs {
				if !keepKeys[normalizePathKey(l.Path)] {
					_ = s.db.DeleteSaveLocation(l.ID)
				}
			}
		}
		// Winner inherited the loser's data but may still carry the heuristic
		// folder name ("Crysis2Remastered"); look up the manifest canonical and
		// rename in place.
		if k, ok := s.bySteamID[appID]; ok {
			canon := s.canonicalKey(k)
			if canon != winner.Name {
				freshWinner, _ := s.db.GetGame(winner.ID)
				if freshWinner != nil {
					s.renameGame(freshWinner, canon)
				}
			}
		}
	}
}

// dedupSaveLocationsPerGame removes save_locations whose path is a strict
// descendant of another save_location of the same game.
func (s *Service) dedupSaveLocationsPerGame() {
	games, err := s.db.ListGames()
	if err != nil {
		return
	}
	for _, g := range games {
		locs, err := s.db.ListSaveLocations(g.ID)
		if err != nil || len(locs) < 2 {
			continue
		}
		cands := make([]pathCand, len(locs))
		for i, l := range locs {
			cands[i] = pathCand{path: l.Path, isDir: true}
		}
		kept := dropDescendants(cands)
		keepKeys := map[string]bool{}
		for _, k := range kept {
			keepKeys[normalizePathKey(k.path)] = true
		}
		for _, l := range locs {
			if !keepKeys[normalizePathKey(l.Path)] {
				_ = s.db.DeleteSaveLocation(l.ID)
			}
		}
	}
}

// normalizePathKey returns a comparable key for a filesystem path that ignores
// trailing separators and case (Windows is case-insensitive).
func normalizePathKey(p string) string {
	return strings.ToLower(strings.TrimRight(filepath.Clean(p), `\/`))
}

func (s *Service) loadManifest() {
	s.manifest = map[string]ManifestEntry{}
	s.byNormalized = map[string]string{}
	s.byTokenKey = map[string]string{}
	s.bySteamID = map[int64]string{}
	s.indexByToken = map[string]string{}
	s.aliasOf = map[string]string{}

	data := ManifestBytes()
	if len(data) == 0 {
		return
	}
	var raw map[string]ManifestEntry
	if err := yaml.Unmarshal(data, &raw); err != nil {
		slog.Warn("parse ludusavi manifest", "err", err)
		return
	}
	// First pass: resolve aliases (up to 4 hops) by inlining the target's fields
	// AND record the alias relationship so we can prefer canonical names later.
	for k, v := range raw {
		entry := v
		canonical := k
		for hops := 0; entry.Alias != "" && hops < 4; hops++ {
			target, ok := raw[entry.Alias]
			if !ok {
				break
			}
			canonical = entry.Alias
			// Inherit non-empty fields from target without overwriting our own.
			if len(entry.Files) == 0 {
				entry.Files = target.Files
			}
			if len(entry.Registry) == 0 {
				entry.Registry = target.Registry
			}
			if entry.Steam == nil {
				entry.Steam = target.Steam
			}
			if entry.GOG == nil {
				entry.GOG = target.GOG
			}
			if len(entry.InstallDir) == 0 {
				entry.InstallDir = target.InstallDir
			}
			entry.Alias = target.Alias
		}
		if canonical != k {
			s.aliasOf[k] = canonical
		}
		s.manifest[k] = entry
	}
	// Second pass: build the lookup indices over resolved entries.
	for k, v := range s.manifest {
		s.byNormalized[normalizeName(k)] = k
		tokens := tokenize(k)
		if len(tokens) > 0 {
			s.byTokenKey[nameKey(tokens)] = k
			s.indexByToken[strings.Join(tokens, " ")] = k
		}
		if v.Steam != nil && v.Steam.ID > 0 {
			s.bySteamID[v.Steam.ID] = k
		}
	}
	slog.Info("loaded ludusavi manifest", "entries", len(s.manifest), "byTokenKeys", len(s.byTokenKey))
}

// buildIndex is a public-ish trigger so discover.go can ensure the index is built.
func (s *Service) buildIndex() { s.once.Do(s.loadManifest) }

// matchGame finds the manifest entry for a Game and creates SaveLocations for
// each existing rendered path. Returns the list of save paths added.
//
// On a confident automatic match the game's display name is updated to the
// manifest's canonical key (e.g. "Crysis2Remastered" → "Crysis 2 Remastered"),
// unless the user has pinned an override (in which case RematchGame already
// set the chosen name).
func (s *Service) matchGame(g *domain.Game) ([]string, error) {
	insts, err := s.db.ListInstallations(g.ID)
	if err != nil {
		return nil, err
	}
	base := ""
	if len(insts) > 0 {
		base = insts[0].RootPath
	}
	gameName := g.Name
	vars := DefaultVars(gameName, base)

	// User override pins this game to a specific manifest entry.
	if key, _ := s.db.GetManifestOverride(g.ID); key != "" {
		if e, ok := s.manifest[key]; ok {
			return s.applyManifestEntry(g, &e, vars), nil
		}
	}
	entry, key := s.bestManifestEntry(g, vars)
	if entry == nil {
		return nil, nil
	}
	added := s.applyManifestEntry(g, entry, vars)
	if key != "" && key != g.Name {
		s.renameGame(g, key)
	}
	return added, nil
}

// renameGame updates the game's display name + slug to the given canonical
// name and clears cached cover paths so meta enrichment refetches them with
// the new (now correct) AppID.
func (s *Service) renameGame(g *domain.Game, newName string) {
	if newName == "" || newName == g.Name {
		return
	}
	g.Name = newName
	g.Slug = util.Slug(newName)
	g.CoverPath = ""
	g.HeroPath = ""
	g.IconPath = ""
	_ = s.db.UpsertGame(g)
}

// applyManifestEntry expands the entry's file patterns under vars and persists
// each resulting on-disk location as a SaveLocation row.
//
//  - Non-glob path that is a directory   → register the directory.
//  - Non-glob path that is a file        → register the file.
//  - Glob whose matches are directories  → register each match (preserves
//                                          per-user-id subfolders etc.).
//  - Glob whose matches are files        → group by parent directory and
//                                          coalesce iff the parent looks like
//                                          a "save folder" (≥ 5 matches OR
//                                          parent total ≤ 10 MB). Otherwise
//                                          register each matched file
//                                          individually — this avoids treating
//                                          e.g. "<base>/base/*.cfg" as the
//                                          whole 655 MB game-data dir.
//  - Child paths whose ancestor is also registered are dropped to keep backups
//    deduplicated.
func (s *Service) applyManifestEntry(g *domain.Game, entry *ManifestEntry, vars TemplateVars) []string {
	cands := []pathCand{}
	seen := map[string]bool{} // normalized path key

	addCand := func(p string, isDir bool) {
		key := normalizePathKey(p)
		if seen[key] {
			return
		}
		seen[key] = true
		cands = append(cands, pathCand{path: p, isDir: isDir})
	}

	for pattern := range entry.Files {
		rendered := Render(pattern, vars)
		isGlob := strings.ContainsAny(pattern, "*?[") || strings.ContainsAny(rendered, "*?")
		matches := expandGlobs(rendered)

		if !isGlob {
			for _, p := range matches {
				st, err := osStat(p)
				if err != nil {
					continue
				}
				addCand(p, st.IsDir())
			}
			continue
		}

		// Split glob matches into dirs vs files.
		dirs := []string{}
		fileByParent := map[string][]string{} // parent (original case) → file list
		for _, p := range matches {
			st, err := osStat(p)
			if err != nil {
				continue
			}
			if st.IsDir() {
				dirs = append(dirs, p)
			} else {
				parent := filepath.Dir(p)
				fileByParent[parent] = append(fileByParent[parent], p)
			}
		}
		for _, d := range dirs {
			addCand(d, true)
		}
		for parent, files := range fileByParent {
			parentSize, _, _ := util.DirSizeAndCount(parent)
			if len(files) >= 5 || parentSize <= 10*1024*1024 {
				// Coalesce to parent directory.
				addCand(parent, true)
			} else {
				// Few matches in a big folder → register matched files individually.
				for _, f := range files {
					addCand(f, false)
				}
			}
		}
	}

	// Drop candidates whose ancestor is also a candidate (avoid double-backup).
	cands = dropDescendants(cands)

	// Coalesce siblings under the same game-namespace dir into their deepest
	// common ancestor — e.g. for Days Gone the three paths
	//   BendGame\Saved\<userid>, BendGame\Saved\Config, BendGame\Saved\Logs
	// collapse into BendGame\Saved\.  Guarded against runaway coalescing into
	// the whole game install dir (PlanetSide 2's *.ini case) by size check.
	cands = coalesceByNamespace(cands)
	cands = dropDescendants(cands)

	added := []string{}
	for _, c := range cands {
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
		if count == 0 {
			continue
		}
		if size > maxSaveBytes {
			slog.Warn("skipping suspiciously large save match", "game", g.Name, "path", c.path, "bytes", size)
			continue
		}
		loc := &domain.SaveLocation{
			ID:           util.SaveLocationID(g.ID, c.path),
			GameID:       g.ID,
			Path:         c.path,
			Kind:         classifySaveKind(c.path),
			SourceHint:   "ludusavi",
			SizeBytes:    size,
			FileCount:    count,
			Mtime:        mtime,
			WatchEnabled: true,
		}
		if err := s.db.UpsertSaveLocation(loc); err != nil {
			slog.Warn("upsert save loc", "err", err)
			continue
		}
		added = append(added, c.path)
	}
	if g.SteamAppID == 0 && entry.Steam != nil && entry.Steam.ID > 0 {
		g.SteamAppID = entry.Steam.ID
		_ = s.db.UpsertGame(g)
	}
	return added
}

// pathCand is the working type for applyManifestEntry; named (rather than
// anonymous struct) so dropDescendants can take []pathCand.
type pathCand struct {
	path  string
	isDir bool
}

// dropDescendants removes candidates whose path lives under another candidate.
// If both /A and /A/B are present, /A/B is dropped so we don't double-back-up.
func dropDescendants(cs []pathCand) []pathCand {
	keys := make([]string, len(cs))
	for i, x := range cs {
		keys[i] = normalizePathKey(x.path)
	}
	keep := make([]bool, len(cs))
	for i := range cs {
		keep[i] = true
	}
	for i, x := range cs {
		if !keep[i] || !x.isDir {
			continue
		}
		prefix := keys[i] + string(filepath.Separator)
		for j := range cs {
			if i == j || !keep[j] {
				continue
			}
			if strings.HasPrefix(keys[j], prefix) {
				keep[j] = false
			}
		}
	}
	out := make([]pathCand, 0, len(cs))
	for i, x := range cs {
		if keep[i] {
			out = append(out, x)
		}
	}
	return out
}

// bestManifestEntry returns the manifest entry that best matches the game and
// also has the most save paths actually present on disk. Scoring beats simple
// name lookup for ambiguous names like "Apex" (which has both an obscure indie
// entry and "Apex Legends" — the latter wins because its paths exist).
func (s *Service) bestManifestEntry(g *domain.Game, vars TemplateVars) (*ManifestEntry, string) {
	// Hard signal: exact Steam AppID match wins without further checks.
	if g.SteamAppID > 0 {
		if k, ok := s.bySteamID[g.SteamAppID]; ok {
			e := s.manifest[k]
			return &e, k
		}
	}
	candidates := s.collectCandidates(g.Name)
	if len(candidates) == 0 {
		return nil, ""
	}
	type scored struct {
		key       string
		entry     *ManifestEntry
		pathHits  int
		tokenDiff int
		nameMatch int // higher = closer
	}
	gameTokens := uniqueTokens(tokenize(g.Name))
	results := make([]scored, 0, len(candidates))
	for _, k := range candidates {
		e := s.manifest[k]
		hits := 0
		for pattern := range e.Files {
			rendered := Render(pattern, vars)
			for _, p := range expandGlobs(rendered) {
				if util.PathExists(p) {
					hits++
				}
			}
		}
		mt := uniqueTokens(tokenize(k))
		nm := 0
		if normalizeName(k) == normalizeName(g.Name) {
			nm = 3
		} else if nameKey(tokenize(k)) == nameKey(tokenize(g.Name)) {
			nm = 2
		} else {
			nm = 1
		}
		results = append(results, scored{
			key: k, entry: &e,
			pathHits:  hits,
			tokenDiff: abs(len(mt) - len(gameTokens)),
			nameMatch: nm,
		})
	}
	// Rank: more on-disk hits first, then closer name match, then smaller token-count diff.
	best := results[0]
	for _, r := range results[1:] {
		better := false
		switch {
		case r.pathHits > best.pathHits:
			better = true
		case r.pathHits == best.pathHits && r.nameMatch > best.nameMatch:
			better = true
		case r.pathHits == best.pathHits && r.nameMatch == best.nameMatch && r.tokenDiff < best.tokenDiff:
			better = true
		}
		if better {
			best = r
		}
	}
	// Guardrail: refuse loose subset matches without any on-disk evidence when
	// the game has > 1 extra unique token vs the manifest. Blocks
	// "Portal with RTX" (3 tokens) → "Portal" (1 token, 2 extras, 0 hits).
	// Keeps "Apex" → "Apex Legends" (0 extras) and "Dead Space (2023)" →
	// "Dead Space" (1 extra).
	if best.pathHits == 0 && best.nameMatch < 2 {
		manifestTokens := uniqueTokens(tokenize(best.key))
		mset := map[string]bool{}
		for _, t := range manifestTokens {
			mset[t] = true
		}
		extra := 0
		for _, t := range gameTokens {
			if !mset[t] {
				extra++
			}
		}
		if extra > 1 {
			return nil, ""
		}
	}
	// If the winner is an alias, prefer its canonical sibling so the game's
	// display name and slug end up using the canonical form (e.g. "Alan Wake 2"
	// → "Alan Wake II"). The entry data is identical because alias resolution
	// inlines it during loadManifest.
	canon := s.canonicalKey(best.key)
	if canon != best.key {
		if e, ok := s.manifest[canon]; ok {
			return &e, canon
		}
	}
	return best.entry, best.key
}

// collectCandidates returns all manifest keys that plausibly match the game name:
// exact normalized, exact token-key, strict subset/superset. We deliberately
// keep this loose — the caller (bestManifestEntry) re-ranks by which entry's
// rendered save paths actually exist on disk, so false positives here are
// harmless. For very common 1-token names ("Apex", "Prey"), this is the only
// way to surface "Apex Legends" / "Prey (2017)" alongside the literal match.
func (s *Service) collectCandidates(name string) []string {
	seen := map[string]bool{}
	out := []string{}
	add := func(k string) {
		if k == "" || seen[k] {
			return
		}
		seen[k] = true
		out = append(out, k)
	}
	if k, ok := s.byNormalized[normalizeName(name)]; ok {
		add(k)
	}
	gameTokens := tokenize(name)
	uniq := uniqueTokens(gameTokens)
	if k, ok := s.byTokenKey[nameKey(gameTokens)]; ok {
		add(k)
	}
	if len(uniq) == 0 {
		return out
	}
	// Superset / subset search. Cap at ~6 tokens to bound work.
	maxLen := len(uniq) + 4
	if maxLen < 4 {
		maxLen = 4
	}
	for k := range s.manifest {
		mt := uniqueTokens(tokenize(k))
		if len(mt) == 0 || len(mt) > maxLen {
			continue
		}
		if setSubset(uniq, mt) || setSubset(mt, uniq) {
			add(k)
		}
	}
	return out
}

// findManifestEntry kept for backward compat with discover.go (returns the
// single best entry without considering path hits).
func (s *Service) findManifestEntry(g *domain.Game) *ManifestEntry {
	e, _ := s.bestManifestEntry(g, DefaultVars(g.Name, ""))
	return e
}

// (Legacy findManifestEntry deleted — bestManifestEntry is the only public path now.)

func uniqueTokens(t []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(t))
	for _, x := range t {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}

// setSubset returns true if every element of a is in b.
func setSubset(a, b []string) bool {
	bs := map[string]bool{}
	for _, x := range b {
		bs[x] = true
	}
	for _, x := range a {
		if !bs[x] {
			return false
		}
	}
	return true
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// (legacy fuzzy lookup removed; discover.go uses lookupByNameStrict instead.)

// resolveSteamName calls the public Steam Web API (no key needed) to get a
// game's display name from its AppID. Empty string on failure.
func (s *Service) resolveSteamName(ctx context.Context, appID int64) string {
	url := fmt.Sprintf("https://store.steampowered.com/api/appdetails?appids=%d", appID)
	client := &http.Client{Timeout: 6 * time.Second}
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return ""
	}
	var body map[string]struct {
		Success bool `json:"success"`
		Data    struct {
			Name string `json:"name"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return ""
	}
	for _, v := range body {
		if v.Success {
			return v.Data.Name
		}
	}
	return ""
}

func expandGlobs(pattern string) []string {
	if !strings.ContainsAny(pattern, "*?[") {
		return []string{pattern}
	}
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil
	}
	return matches
}

func classifySaveKind(path string) domain.SaveKind {
	low := strings.ToLower(path)
	switch {
	case strings.Contains(low, "saved games"):
		return domain.SaveKindSavedGames
	case strings.Contains(low, "documents") && strings.Contains(low, "my games"):
		return domain.SaveKindMyGames
	case strings.Contains(low, `appdata\local\`):
		return domain.SaveKindAppDataLocal
	case strings.Contains(low, `appdata\locallow\`):
		return domain.SaveKindAppDataLow
	case strings.Contains(low, `appdata\roaming\`):
		return domain.SaveKindAppDataRoaming
	case strings.Contains(low, "documents"):
		return domain.SaveKindDocuments
	}
	return domain.SaveKindCustom
}

func normalizeName(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, "™", "")
	s = strings.ReplaceAll(s, "®", "")
	s = strings.TrimSpace(s)
	return s
}
