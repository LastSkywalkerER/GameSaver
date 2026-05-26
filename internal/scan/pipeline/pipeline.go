package pipeline

import (
	"context"
	"log/slog"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"GameSaver/internal/config"
	"GameSaver/internal/domain"
	"GameSaver/internal/scan/heuristic"
	"GameSaver/internal/scan/launchers"
	"GameSaver/internal/storage/sqlite"
	"GameSaver/internal/util"
)

// Emit is a function that publishes a UI event.
type Emit func(event string, payload any)

// Result is the summary returned to the UI when a scan completes.
type Result struct {
	GamesUpserted      int            `json:"gamesUpserted"`
	InstallationsTotal int            `json:"installationsTotal"`
	PerSource          map[string]int `json:"perSource"`
	DurationMS         int64          `json:"durationMs"`
}

// Pipeline orchestrates all scanners and persists to DB.
type Pipeline struct {
	db   *sqlite.Store
	cfg  *config.Config
	emit Emit
}

func New(db *sqlite.Store, cfg *config.Config, emit Emit) *Pipeline {
	return &Pipeline{db: db, cfg: cfg, emit: emit}
}

func (p *Pipeline) Run(ctx context.Context) (*Result, error) {
	start := time.Now()
	res := &Result{PerSource: map[string]int{}}

	scanners := []launchers.Scanner{
		launchers.SteamScanner{},
		launchers.GOGScanner{},
		launchers.EpicScanner{},
		launchers.EAScanner{},
		launchers.UbisoftScanner{},
		launchers.BattleNetScanner{},
		launchers.XboxScanner{},
		launchers.MinecraftScanner{},
	}

	allItems := []*launchers.ScanItem{}
	knownExe := map[string]bool{}
	knownRoot := map[string]bool{}

	for _, sc := range scanners {
		if ctx.Err() != nil {
			break
		}
		p.fire("scan:progress", map[string]any{"phase": "launcher", "name": sc.Name()})
		items, err := sc.Scan(ctx)
		if err != nil {
			slog.Warn("scanner failed", "name", sc.Name(), "err", err)
		}
		for _, it := range items {
			if it == nil {
				continue
			}
			knownExe[strings.ToLower(it.ExePath)] = true
			knownRoot[strings.ToLower(filepath.Clean(it.RootPath))] = true
			allItems = append(allItems, it)
			res.PerSource[sc.Name()]++
		}
		p.fire("scan:source-done", map[string]any{"name": sc.Name(), "count": res.PerSource[sc.Name()]})
	}

	// Heuristic pass over disks for standalone games — skipping anything a
	// launcher already claimed.
	p.fire("scan:progress", map[string]any{"phase": "heuristic"})
	h := heuristic.New()
	h.Known = knownExe
	h.KnownRoots = knownRoot
	if items, err := h.Scan(ctx); err == nil {
		for _, it := range items {
			if it == nil {
				continue
			}
			rootKey := strings.ToLower(filepath.Clean(it.RootPath))
			if knownRoot[rootKey] {
				continue
			}
			allItems = append(allItems, it)
			knownRoot[rootKey] = true
			res.PerSource["standalone"]++
		}
	}

	// Collapse duplicates: if multiple scan items share the same RootPath,
	// merge into one "best" item (priority by source) before grouping.
	allItems = dedupByRootPath(allItems)

	// Group items into Games — prefer GameKey, then Steam AppID, then slug of name.
	groups := map[string][]*launchers.ScanItem{}
	for _, it := range allItems {
		key := it.GameKey
		if key == "" {
			key = aliasKey(it.Name)
		}
		if key == "" {
			if it.SteamAppID > 0 {
				key = "steam:" + strconv.FormatInt(it.SteamAppID, 10)
			} else {
				key = util.Slug(it.Name)
			}
		}
		groups[key] = append(groups[key], it)
	}

	// Persist
	for key, items := range groups {
		if ctx.Err() != nil {
			break
		}
		name := pickDisplayName(items)
		slug := util.Slug(name)
		// Stable game ID derived from canonical slug + best secondary identifier.
		gameID := util.GameID(slug, primaryKey(items))
		g := &domain.Game{
			ID:         gameID,
			Name:       name,
			Slug:       slug,
			SteamAppID: pickSteamAppID(items),
		}
		_ = key // grouping key is internal-only
		if err := p.db.UpsertGame(g); err != nil {
			slog.Warn("upsert game", "name", name, "err", err)
			continue
		}
		res.GamesUpserted++
		p.fire("scan:game", map[string]any{"id": gameID, "name": name})
		for _, it := range items {
			inst := &domain.Installation{
				ID:          it.InstallID,
				GameID:      gameID,
				Source:      it.Source,
				SourceAppID: it.SourceAppID,
				RootPath:    it.RootPath,
				ExePath:     it.ExePath,
				SizeBytes:   it.SizeBytes,
				LastSeenAt:  it.LastSeenAt,
				LaunchURI:   it.LaunchURI,
			}
			if err := p.db.UpsertInstallation(inst); err != nil {
				slog.Warn("upsert installation", "exe", it.ExePath, "err", err)
				continue
			}
			res.InstallationsTotal++
		}
	}

	res.DurationMS = time.Since(start).Milliseconds()
	p.fire("scan:done", res)
	return res, nil
}

func (p *Pipeline) fire(ev string, payload any) {
	if p.emit != nil {
		p.emit(ev, payload)
	}
}

// aliasKey returns a stable grouping key for games that ship under wildly
// different names across launchers (e.g. Minecraft has 4+ distinct names).
// Empty string means "no alias — fall through to default keys".
func aliasKey(name string) string {
	low := strings.ToLower(name)
	switch {
	case strings.Contains(low, "minecraft"):
		return "minecraft"
	}
	return ""
}

// pickDisplayName chooses the best Name from a set of items: prefer the launcher
// that's most authoritative for game metadata.
func pickDisplayName(items []*launchers.ScanItem) string {
	priority := map[domain.SourceKind]int{
		domain.SourceSteam:     10,
		domain.SourceGOG:       9,
		domain.SourceEpic:      8,
		domain.SourceEA:        7,
		domain.SourceUbisoft:   6,
		domain.SourceBattleNet: 6,
		domain.SourceXbox:      5,
		domain.SourceMinecraft: 5,
	}
	bestScore := -1
	best := items[0].Name
	for _, it := range items {
		if s, ok := priority[it.Source]; ok && s > bestScore {
			bestScore = s
			best = it.Name
		}
	}
	return best
}

func pickSteamAppID(items []*launchers.ScanItem) int64 {
	for _, it := range items {
		if it.SteamAppID > 0 {
			return it.SteamAppID
		}
	}
	return 0
}

// primaryKey returns a stable secondary key for game ID derivation. Prefers
// Steam AppID (stable across renames), then the GameKey, then root path.
func primaryKey(items []*launchers.ScanItem) string {
	if id := pickSteamAppID(items); id > 0 {
		return "steam:" + strconv.FormatInt(id, 10)
	}
	for _, it := range items {
		if it.GameKey != "" {
			return "key:" + it.GameKey
		}
	}
	return strings.ToLower(items[0].RootPath)
}

// dedupByRootPath collapses items pointing at the same physical install dir.
// The winner is the highest-priority source; metadata (Steam AppID, LaunchURI)
// from losing entries is preserved when missing on the winner.
func dedupByRootPath(items []*launchers.ScanItem) []*launchers.ScanItem {
	priority := map[domain.SourceKind]int{
		domain.SourceSteam:     10,
		domain.SourceGOG:       9,
		domain.SourceEpic:      8,
		domain.SourceEA:        7,
		domain.SourceUbisoft:   6,
		domain.SourceBattleNet: 6,
		domain.SourceXbox:      5,
		domain.SourceMinecraft: 5,
		domain.SourceStandalone: 1,
	}
	type group struct {
		winner *launchers.ScanItem
		all    []*launchers.ScanItem
	}
	groups := map[string]*group{}
	for _, it := range items {
		key := strings.ToLower(filepath.Clean(it.RootPath))
		g, ok := groups[key]
		if !ok {
			groups[key] = &group{winner: it, all: []*launchers.ScanItem{it}}
			continue
		}
		g.all = append(g.all, it)
		if priority[it.Source] > priority[g.winner.Source] {
			g.winner = it
		}
	}
	out := make([]*launchers.ScanItem, 0, len(groups))
	for _, g := range groups {
		w := g.winner
		// Merge metadata from losers where winner is missing it.
		for _, it := range g.all {
			if it == w {
				continue
			}
			if w.SteamAppID == 0 && it.SteamAppID > 0 {
				w.SteamAppID = it.SteamAppID
			}
			if w.LaunchURI == "" && it.LaunchURI != "" {
				w.LaunchURI = it.LaunchURI
			}
			if w.SourceAppID == "" && it.SourceAppID != "" {
				w.SourceAppID = it.SourceAppID
			}
		}
		out = append(out, w)
	}
	return out
}
