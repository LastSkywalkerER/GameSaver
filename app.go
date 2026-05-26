package main

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"GameSaver/internal/backup"
	"GameSaver/internal/config"
	"GameSaver/internal/domain"
	"GameSaver/internal/launcher"
	"GameSaver/internal/match"
	"GameSaver/internal/meta"
	"GameSaver/internal/scan/pipeline"
	"GameSaver/internal/storage/sqlite"
)

// App is the Wails-bound facade. All exported methods become callable from the
// React frontend via the generated bindings.
type App struct {
	cfg    *config.Config
	ctx    context.Context
	db     *sqlite.Store
	meta   *meta.Service
	bk     *backup.Engine
	match  *match.Service
	launch *launcher.Service

	scanMu sync.Mutex
}

func NewApp(cfg *config.Config) *App {
	return &App{cfg: cfg}
}

func (a *App) Startup(ctx context.Context) {
	a.ctx = ctx
	db, err := sqlite.Open(a.cfg.DBPath())
	if err != nil {
		slog.Error("open db", "err", err)
		return
	}
	a.db = db
	a.meta = meta.New(a.cfg, db)
	a.match = match.New(db)
	a.bk = backup.New(a.cfg, db)
	a.launch = launcher.New(db)

	// Sync DB with on-disk backups: re-import orphan snapshot zips into the
	// snapshots table (and prune dead rows). Runs on every start; idempotent.
	go func() {
		r, err := a.bk.Reconcile(ctx)
		if err != nil {
			slog.Warn("startup reconcile", "err", err)
			return
		}
		slog.Info("startup reconcile done",
			"scanned", r.ScannedManifests,
			"imported", r.ImportedSnaps,
			"alreadyKnown", r.AlreadyKnown,
			"createdGames", r.CreatedGames,
			"createdLocations", r.CreatedLocations,
			"orphanZips", r.OrphanZips,
			"orphanManifests", r.OrphanManifests,
			"deadSnapshots", r.DeadSnapshots)
		if a.ctx != nil && (r.ImportedSnaps > 0 || r.DeadSnapshots > 0) {
			wailsruntime.EventsEmit(a.ctx, "reconcile:done", r)
		}
	}()
	slog.Info("startup complete")
}

func (a *App) Shutdown(_ context.Context) {
	if a.db != nil {
		_ = a.db.Close()
	}
}

// ===== Config =====

func (a *App) GetConfig() *config.Config { return a.cfg }

func (a *App) SetBackupRoot(path string) error {
	a.cfg.BackupRoot = path
	return config.Save(a.cfg)
}

func (a *App) SetSteamGridDBKey(key string) error {
	a.cfg.SteamGridDBKey = strings.TrimSpace(key)
	return config.Save(a.cfg)
}

func (a *App) SetLanguage(lang string) error {
	a.cfg.Language = lang
	return config.Save(a.cfg)
}

func (a *App) SetTheme(theme string) error {
	a.cfg.Theme = theme
	return config.Save(a.cfg)
}

// ===== Scan =====

func (a *App) ScanLibrary() (*pipeline.Result, error) {
	if !a.scanMu.TryLock() {
		return nil, fmt.Errorf("scan already in progress")
	}
	defer a.scanMu.Unlock()

	emit := func(ev string, payload any) {
		if a.ctx != nil {
			wailsruntime.EventsEmit(a.ctx, ev, payload)
		}
	}

	p := pipeline.New(a.db, a.cfg, emit)
	res, err := p.Run(a.ctx)
	if err != nil {
		return nil, err
	}

	// Match save locations using Ludusavi manifest for installed games.
	if err := a.match.MatchAll(a.ctx, emit); err != nil {
		slog.Error("match saves", "err", err)
	}

	// Reverse-scan: walk the entire manifest and surface orphan saves
	// (games whose folders exist on disk even when not installed).
	if _, err := a.match.ReverseScan(a.ctx, emit); err != nil {
		slog.Error("reverse scan", "err", err)
	}

	// Fetch covers/metadata.
	a.meta.EnrichAll(a.ctx, emit)

	return res, nil
}

// ===== Games / Library =====

func (a *App) ListGames() ([]*domain.GameView, error) {
	games, err := a.db.ListGames()
	if err != nil {
		return nil, err
	}
	out := make([]*domain.GameView, 0, len(games))
	for _, g := range games {
		gv, err := a.gameView(g)
		if err != nil {
			slog.Warn("build game view", "id", g.ID, "err", err)
			continue
		}
		out = append(out, gv)
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Game.Name) < strings.ToLower(out[j].Game.Name)
	})
	return out, nil
}

func (a *App) GetGame(id string) (*domain.GameView, error) {
	g, err := a.db.GetGame(id)
	if err != nil {
		return nil, err
	}
	return a.gameView(g)
}

func (a *App) gameView(g *domain.Game) (*domain.GameView, error) {
	insts, err := a.db.ListInstallations(g.ID)
	if err != nil {
		return nil, err
	}
	saves, err := a.db.ListSaveLocations(g.ID)
	if err != nil {
		return nil, err
	}
	snaps, err := a.db.ListSnapshots(g.ID)
	if err != nil {
		return nil, err
	}
	return &domain.GameView{
		Game:          g,
		Installations: insts,
		SaveLocations: saves,
		Snapshots:     snaps,
	}, nil
}

func (a *App) HideGame(id string, hidden bool) error {
	return a.db.SetGameHidden(id, hidden)
}

// ===== Match: search, manual override, deep scan =====

func (a *App) SearchManifest(query string, limit int) []match.ManifestSearchResult {
	return a.match.SearchManifest(query, limit)
}

func (a *App) RematchGame(gameID, manifestKey string) error {
	if err := a.match.RematchGame(a.ctx, gameID, manifestKey); err != nil {
		return err
	}
	// Refresh metadata for the changed game (cover, etc.)
	if g, err := a.db.GetGame(gameID); err == nil {
		_ = a.meta.EnrichGame(a.ctx, g)
	}
	return nil
}

func (a *App) ClearManifestOverride(gameID string) error {
	return a.match.RematchGame(a.ctx, gameID, "")
}

func (a *App) AddManualSaveLocation(gameID, path string) (*domain.SaveLocation, error) {
	return a.match.AddManualSaveLocation(gameID, path)
}

func (a *App) RemoveSaveLocation(locationID string) error {
	return a.match.RemoveSaveLocation(locationID)
}

func (a *App) DeepScanGame(gameID string) ([]*match.DeepScanResult, error) {
	return a.match.DeepScanGame(gameID)
}

// ReconcileBackups walks the backup folder, re-imports snapshots from manifest
// sidecars, and prunes DB rows whose archive files are gone. Safe to call any
// time; idempotent.
func (a *App) ReconcileBackups() (*backup.ReconcileResult, error) {
	res, err := a.bk.Reconcile(a.ctx)
	if err != nil {
		return nil, err
	}
	if a.ctx != nil {
		wailsruntime.EventsEmit(a.ctx, "reconcile:done", res)
	}
	return res, nil
}

// ReverseScanSaves walks the entire Ludusavi manifest and creates game cards
// for any save folders found on disk, even when the game itself is not installed.
func (a *App) ReverseScanSaves() (*match.ReverseScanResult, error) {
	emit := func(ev string, payload any) {
		if a.ctx != nil {
			wailsruntime.EventsEmit(a.ctx, ev, payload)
		}
	}
	res, err := a.match.ReverseScan(a.ctx, emit)
	if err != nil {
		return nil, err
	}
	// Pull covers for any new games.
	a.meta.EnrichAll(a.ctx, emit)
	return res, nil
}

// ===== Backup =====

func (a *App) BackupGame(gameID string) ([]*domain.Snapshot, error) {
	return a.bk.BackupGame(a.ctx, gameID, domain.TriggerManual)
}

func (a *App) BackupAll() (int, error) {
	return a.bk.BackupAll(a.ctx, domain.TriggerManual)
}

func (a *App) RestoreSnapshot(snapshotID string, overwrite bool) error {
	return a.bk.Restore(a.ctx, snapshotID, overwrite)
}

func (a *App) DeleteSnapshot(snapshotID string) error {
	return a.bk.DeleteSnapshot(snapshotID)
}

func (a *App) OpenBackupFolder() error {
	return openInExplorer(a.cfg.BackupRoot)
}

// ===== Launch =====

func (a *App) LaunchGame(gameID string, installationID string) error {
	return a.launch.Launch(gameID, installationID)
}

// ===== Misc =====

func (a *App) AppVersion() string { return AppVersion }

func (a *App) PickFolder(title string) (string, error) {
	if a.ctx == nil {
		return "", fmt.Errorf("no context")
	}
	return wailsruntime.OpenDirectoryDialog(a.ctx, wailsruntime.OpenDialogOptions{
		Title: title,
	})
}

func (a *App) ShowItemInFolder(path string) error { return openInExplorer(path) }

func (a *App) Toast(level, message string) {
	if a.ctx == nil {
		return
	}
	wailsruntime.EventsEmit(a.ctx, "toast", map[string]string{
		"level":   level,
		"message": message,
	})
}
