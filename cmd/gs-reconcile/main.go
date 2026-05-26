// gs-reconcile runs Reconcile once and prints the result. Used to verify the
// startup pass without launching the full Wails app.
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
	"GameSaver/internal/storage/sqlite"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	_ = logging.Setup(cfg)

	db, err := sqlite.Open(cfg.DBPath())
	if err != nil {
		fmt.Fprintln(os.Stderr, "open db:", err)
		os.Exit(1)
	}
	defer db.Close()

	eng := backup.New(cfg, db)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	res, err := eng.Reconcile(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "reconcile:", err)
		os.Exit(1)
	}
	fmt.Printf("Scanned manifests:    %d\n", res.ScannedManifests)
	fmt.Printf("Imported snapshots:   %d\n", res.ImportedSnaps)
	fmt.Printf("Already known:        %d\n", res.AlreadyKnown)
	fmt.Printf("Created games:        %d\n", res.CreatedGames)
	fmt.Printf("Created locations:    %d\n", res.CreatedLocations)
	fmt.Printf("Orphan zips:          %d\n", res.OrphanZips)
	fmt.Printf("Orphan manifests:     %d\n", res.OrphanManifests)
	fmt.Printf("Deleted dead rows:    %d\n", res.DeadSnapshots)
	slog.Info("reconcile done", "result", res)
}
