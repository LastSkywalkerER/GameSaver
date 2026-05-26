// gs-smoke runs the full scan + match + meta pipeline once from the command line
// and prints a summary. Useful for catching backend bugs without touching the UI.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"GameSaver/internal/backup"
	"GameSaver/internal/config"
	"GameSaver/internal/logging"
	"GameSaver/internal/match"
	"GameSaver/internal/meta"
	"GameSaver/internal/scan/pipeline"
	"GameSaver/internal/storage/sqlite"
)

func main() {
	cfg, err := config.Load()
	must(err, "load config")
	if err := logging.Setup(cfg); err != nil {
		slog.Warn("logging setup", "err", err)
	}

	db, err := sqlite.Open(cfg.DBPath())
	must(err, "open db")
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	quietEmit := func(ev string, payload any) {} // silent — too noisy for the smoke
	loudEmit := func(ev string, payload any) {
		fmt.Printf("[event] %s %v\n", ev, payload)
	}

	fmt.Println("== scan ==")
	pl := pipeline.New(db, cfg, quietEmit)
	t0 := time.Now()
	res, err := pl.Run(ctx)
	must(err, "scan")
	fmt.Printf("scan done in %s: games=%d installs=%d perSource=%v\n",
		time.Since(t0), res.GamesUpserted, res.InstallationsTotal, res.PerSource)

	fmt.Println("== match ==")
	m := match.New(db)
	must(m.MatchAll(ctx, quietEmit), "match")

	if len(os.Args) > 1 && os.Args[1] == "--reverse" {
		fmt.Println("== reverse scan ==")
		rev, err := m.ReverseScan(ctx, loudEmit)
		must(err, "reverse")
		fmt.Printf("reverse done in %dms: checked=%d matched=%d new games=%d new locs=%d\n",
			rev.DurationMS, rev.EntriesChecked, rev.EntriesMatched, rev.NewGamesCreated, rev.NewLocationsAdded)
	}

	fmt.Println("== meta (covers) ==")
	mt := meta.New(cfg, db)
	mt.EnrichAll(ctx, quietEmit)

	games, err := db.ListGames()
	must(err, "list games")
	fmt.Printf("\n== games (%d) ==\n", len(games))
	for _, g := range games {
		insts, _ := db.ListInstallations(g.ID)
		locs, _ := db.ListSaveLocations(g.ID)
		fmt.Printf("- %-45s installs=%d saves=%d steam=%d cover=%q\n",
			truncate(g.Name, 45), len(insts), len(locs), g.SteamAppID, g.CoverPath)
		for _, l := range locs {
			fmt.Printf("    save: [%s/%s] %s (%d files, %d B)\n", l.Kind, l.SourceHint, l.Path, l.FileCount, l.SizeBytes)
		}
	}

	if len(os.Args) > 1 && os.Args[1] == "--backup" {
		fmt.Println("\n== backup all ==")
		eng := backup.New(cfg, db)
		n, err := eng.BackupAll(ctx, "manual")
		must(err, "backup all")
		fmt.Printf("created %d snapshots\n", n)
	}
}

func must(err error, what string) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "FAIL %s: %v\n", what, err)
		os.Exit(1)
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
