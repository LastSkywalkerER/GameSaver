package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"GameSaver/internal/backup"
	"GameSaver/internal/config"
	"GameSaver/internal/controller"
	"GameSaver/internal/domain"
	"GameSaver/internal/launcher"
	"GameSaver/internal/match"
	"GameSaver/internal/meta"
	"GameSaver/internal/playtime"
	"GameSaver/internal/scan/dirsize"
	"GameSaver/internal/scan/pipeline"
	"GameSaver/internal/shellmode"
	"GameSaver/internal/storage/sqlite"
	"GameSaver/internal/tray"
	"GameSaver/internal/updater"
	"GameSaver/internal/watcher"
)

// App is the Wails-bound facade. All exported methods become callable from the
// React frontend via the generated bindings.
type App struct {
	cfg     *config.Config
	ctx     context.Context
	db      *sqlite.Store
	meta    *meta.Service
	bk      *backup.Engine
	match   *match.Service
	launch  *launcher.Service
	updater  *updater.Updater
	watcher  *watcher.Service
	playtime *playtime.Service

	scanMu sync.Mutex
}

// Context returns the Wails runtime context once Startup has fired.
// Exposed so satellite goroutines (tray, watcher) can drive runtime events.
func (a *App) Context() context.Context { return a.ctx }

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
	a.updater = updater.New(AppVersion)
	a.watcher = watcher.New(a.cfg, a.db, a.bk)
	a.playtime = playtime.New(a.db)
	a.playtime.Start(ctx)

	// Clean up the previous binary that minio/selfupdate leaves behind as a
	// .<name>.old rollback file. We're now running the new exe, so the old
	// one is safe to remove. Best-effort — never blocks startup.
	cleanupOldExe()

	// Auto-backup watcher: starts if user toggled it on (default false).
	if a.cfg.WatcherEnabled {
		if err := a.watcher.Start(ctx); err != nil {
			slog.Warn("watcher start", "err", err)
		}
	}

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

	// Background update check (after a short delay so the UI is responsive).
	if a.cfg.AutoCheckUpdates {
		go a.backgroundUpdateCheck()
	}

	// Catch up on install-dir sizes for installs that have never been measured
	// or whose value is stale (>24h). Runs after a small delay so it doesn't
	// fight startup I/O.
	go func() {
		time.Sleep(15 * time.Second)
		if a.ctx == nil {
			return
		}
		emit := func(ev string, payload any) { wailsruntime.EventsEmit(a.ctx, ev, payload) }
		ds := dirsize.New(a.db, emit)
		ds.Run(a.ctx, false)
	}()

	// XInput poller — emits controller:state/button/nav so the UI can show
	// a 🎮 chip and drive d-pad menu navigation. Cheap when no controller
	// connected (one syscall per 20 ms returning ERROR_DEVICE_NOT_CONNECTED).
	go func() {
		emit := func(ev string, payload any) { wailsruntime.EventsEmit(a.ctx, ev, payload) }
		controller.New(emit).Run(a.ctx)
	}()

	slog.Info("startup complete", "version", AppVersion)
}

// backgroundUpdateCheck waits a short ramp-up time, queries GitHub, then
// repeats every 30 minutes for the lifetime of the process. Each tick emits
// "update:available" if a newer version exists that the user hasn't
// permanently skipped. Respects the AutoCheckUpdates toggle on every tick so
// users can pause/resume it without restarting.
func (a *App) backgroundUpdateCheck() {
	// Initial delay so the UI is interactive before the first network call.
	select {
	case <-time.After(8 * time.Second):
	case <-a.ctx.Done():
		return
	}
	a.runUpdateCheck(false)

	// Recurring tick. 30 min matches user expectation of "checks on its own
	// every so often" without hammering GitHub.
	t := time.NewTicker(30 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-t.C:
			if !a.cfg.AutoCheckUpdates {
				continue
			}
			a.runUpdateCheck(false)
		}
	}
}

// runUpdateCheck hits GitHub, persists the last-check timestamp, and emits
// "update:available" if a newer (non-skipped) version exists. Shared by the
// startup goroutine, the 30-min ticker, and the manual Settings button so all
// three paths behave identically. `manual=true` bypasses the SkippedUpdateVer
// gate because the user explicitly asked.
func (a *App) runUpdateCheck(manual bool) {
	if a.ctx == nil {
		return
	}
	info, err := a.updater.Check(a.ctx)
	if err != nil {
		slog.Info("update check failed (non-fatal)", "err", err)
		return
	}
	a.cfg.LastUpdateCheckUnix = time.Now().Unix()
	_ = config.Save(a.cfg)
	if !info.Available {
		return
	}
	if !manual && a.cfg.SkippedUpdateVer != "" && a.cfg.SkippedUpdateVer == info.LatestVer {
		slog.Info("update available but skipped by user", "version", info.LatestVer)
		return
	}
	slog.Info("update available", "current", info.CurrentVer, "latest", info.LatestVer)
	wailsruntime.EventsEmit(a.ctx, "update:available", info)
}

func (a *App) Shutdown(_ context.Context) {
	if a.playtime != nil {
		a.playtime.Stop()
	}
	if a.watcher != nil {
		a.watcher.Stop()
	}
	tray.Quit()
	if a.db != nil {
		_ = a.db.Close()
	}
}

// ListPlaySessions returns recent play sessions for a game (newest first).
func (a *App) ListPlaySessions(gameID string, limit int) ([]*domain.PlaySession, error) {
	return a.db.ListSessions(gameID, limit)
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

	// Compute install-dir sizes in the background — slow for big games on HDD,
	// so we don't block the scan result. UI updates row-by-row via "inst:size".
	go func() {
		ds := dirsize.New(a.db, emit)
		ds.Run(a.ctx, false)
	}()

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

// ===== Updater =====

func (a *App) CheckForUpdate() (*updater.UpdateInfo, error) {
	info, err := a.updater.Check(a.ctx)
	if err != nil {
		return nil, err
	}
	a.cfg.LastUpdateCheckUnix = time.Now().Unix()
	_ = config.Save(a.cfg)
	// Mirror the background-check path: emit "update:available" so the banner
	// actually shows up. The Settings button's "баннер появится сверху" toast
	// was previously a lie because we only returned the info. The manual flag
	// bypasses the SkippedUpdateVer gate — if the user clicked "Проверить
	// сейчас" they want to see the banner even for a version they once skipped.
	if info != nil && info.Available && a.ctx != nil {
		wailsruntime.EventsEmit(a.ctx, "update:available", info)
	}
	return info, nil
}

// ApplyUpdate downloads the latest release zip, verifies its checksum,
// replaces the running binary and returns. The caller (UI) is expected to
// trigger a restart afterwards.
func (a *App) ApplyUpdate() error {
	info, err := a.updater.Check(a.ctx)
	if err != nil {
		return err
	}
	if !info.Available {
		return fmt.Errorf("no update available")
	}
	if err := a.updater.Apply(a.ctx, info); err != nil {
		return err
	}
	// Clear the skipped-version marker — user just accepted an upgrade.
	a.cfg.SkippedUpdateVer = ""
	_ = config.Save(a.cfg)
	return nil
}

// SkipUpdate pins the user's decision to skip a specific released version.
// The startup check won't surface that exact version again.
func (a *App) SkipUpdate(version string) error {
	a.cfg.SkippedUpdateVer = version
	return config.Save(a.cfg)
}

// SetAutoCheckUpdates toggles whether the startup pass hits GitHub.
func (a *App) SetAutoCheckUpdates(enabled bool) error {
	a.cfg.AutoCheckUpdates = enabled
	return config.Save(a.cfg)
}

// ===== Watcher controls (used by Settings UI + tray) =====

// IsWatcherEnabled reports the in-memory running state of the auto-backup watcher.
func (a *App) IsWatcherEnabled() bool {
	return a.watcher != nil && a.watcher.IsRunning()
}

// WatcherToggle starts or stops the watcher and persists the new state in
// settings.json so it survives restarts. Used by both the tray menu checkbox
// and the Settings page.
func (a *App) WatcherToggle(enabled bool) error {
	a.cfg.WatcherEnabled = enabled
	if err := config.Save(a.cfg); err != nil {
		return err
	}
	if a.watcher == nil {
		return nil
	}
	if enabled {
		if err := a.watcher.Start(a.ctx); err != nil {
			return err
		}
	} else {
		a.watcher.Stop()
	}
	tray.SyncWatcherState(enabled)
	return nil
}

// SetWatcherDebounceMinutes lets the UI tune how long of no-changes triggers
// an auto-backup. Min 1 minute, max 24 hours. Persisted to settings.json.
func (a *App) SetWatcherDebounceMinutes(minutes int) error {
	if minutes < 1 {
		minutes = 1
	}
	if minutes > 24*60 {
		minutes = 24 * 60
	}
	a.cfg.WatcherDebounceMs = minutes * 60 * 1000
	return config.Save(a.cfg)
}

// SetRetentionKeepN configures how many snapshots per save location the
// backup engine keeps before pruning oldest. 0 = unlimited; clamps to 1..1000.
func (a *App) SetRetentionKeepN(n int) error {
	if n < 0 {
		n = 0
	}
	if n > 1000 {
		n = 1000
	}
	a.cfg.RetentionKeepN = n
	return config.Save(a.cfg)
}

// cleanupOldExe removes ".<exename>.old" next to the running executable, which
// is the rollback artefact left by minio/selfupdate after a successful Apply.
// Called from Startup so the file is gone by the first paint of the new build.
func cleanupOldExe() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	dir := filepath.Dir(exe)
	name := filepath.Base(exe)
	// selfupdate's default name: "." + base + ".old", e.g. ".GameSaver.exe.old".
	candidates := []string{
		filepath.Join(dir, "."+name+".old"),
		filepath.Join(dir, name+".old"),
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			if rmErr := os.Remove(p); rmErr == nil {
				slog.Info("removed stale update rollback file", "path", p)
			} else {
				slog.Warn("could not remove rollback file", "path", p, "err", rmErr)
			}
		}
	}
}

// RestartApp re-launches the process with the same arguments and exits the
// current one. Used after Apply() to switch into the freshly written exe.
func (a *App) RestartApp() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, os.Args[1:]...)
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		return err
	}
	// Give the new process a moment to take the window, then bow out.
	go func() {
		time.Sleep(500 * time.Millisecond)
		os.Exit(0)
	}()
	return nil
}

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

// ===== Shell mode (HKCU\...\Winlogon\Shell) =====
//
// On enable we download the small watchdog binary from the latest GitHub
// release into %LOCALAPPDATA%\GameSaver\bin\, write the current
// GameSaver.exe path next to it, and point the registry at the watchdog.
// On disable we just nuke the registry value — the watchdog stays on disk
// so re-enable is instant.

// ShellModeStatus is the structured payload returned to the UI so a single
// poll gives the full picture (downloaded? registered? running under shell?).
type ShellModeStatus struct {
	WatchdogPresent bool `json:"watchdogPresent"`
	Registered      bool `json:"registered"`
	RunningAsShell  bool `json:"runningAsShell"`
}

func (a *App) GetShellModeStatus() (*ShellModeStatus, error) {
	paths, err := shellmode.ResolvePaths()
	if err != nil {
		return nil, err
	}
	reg, err := shellmode.IsRegistered()
	if err != nil {
		return nil, err
	}
	return &ShellModeStatus{
		WatchdogPresent: paths.WatchdogPresent(),
		Registered:      reg,
		RunningAsShell:  os.Getenv("GS_SHELL_MODE") == "1",
	}, nil
}

// EnableShellMode downloads the watchdog if missing, then writes the
// HKCU\...\Winlogon\Shell registry value. The UI is expected to have
// shown the user a big scary warning before calling this.
func (a *App) EnableShellMode() error {
	if _, err := shellmode.EnsureWatchdog(a.ctx); err != nil {
		return fmt.Errorf("download watchdog: %w", err)
	}
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve self exe: %w", err)
	}
	if err := shellmode.Enable(exe); err != nil {
		return fmt.Errorf("write registry: %w", err)
	}
	return nil
}

// DisableShellMode clears the registry value. If we're currently running
// under the watchdog the UI is expected to follow up with QuitApp() — that
// gives the user Explorer back immediately without needing to log out and
// back in (watchdog exits on clean-exit and launches explorer.exe itself).
func (a *App) DisableShellMode() error {
	return shellmode.Disable()
}

// QuitApp triggers a clean Wails shutdown. Used by the "Exit shell mode"
// button so the watchdog (which is our parent in shell mode) sees an exit
// code of 0, declines to restart, and brings Explorer back up. Safe to
// call in normal mode too — just quits the app like the tray "Quit" item.
func (a *App) QuitApp() {
	if a.ctx == nil {
		return
	}
	wailsruntime.Quit(a.ctx)
}
