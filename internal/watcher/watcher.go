// Package watcher uses fsnotify to observe every known save_location on disk
// and triggers a backup snapshot after the configured period of inactivity.
// Defers the snapshot if any installation of the affected game has its exe
// running (file might be open/locked).
package watcher

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"GameSaver/internal/backup"
	"GameSaver/internal/config"
	"GameSaver/internal/domain"
	"GameSaver/internal/storage/sqlite"
)

// Service is the per-app watcher. Safe to Start/Stop repeatedly (e.g. from
// the Settings toggle).
type Service struct {
	cfg *config.Config
	db  *sqlite.Store
	bk  *backup.Engine

	mu      sync.Mutex
	fsw     *fsnotify.Watcher
	timers  map[string]*time.Timer // location ID → debounce timer
	watched map[string]string      // watched dir → location ID
	cancel  context.CancelFunc
	running bool
}

func New(cfg *config.Config, db *sqlite.Store, bk *backup.Engine) *Service {
	return &Service{
		cfg:     cfg,
		db:      db,
		bk:      bk,
		timers:  map[string]*time.Timer{},
		watched: map[string]string{},
	}
}

// Start spins up the watcher; safe to call when already running (no-op).
func (s *Service) Start(parent context.Context) error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return nil
	}
	w, err := fsnotify.NewWatcher()
	if err != nil {
		s.mu.Unlock()
		return err
	}
	ctx, cancel := context.WithCancel(parent)
	s.fsw = w
	s.cancel = cancel
	s.running = true
	s.mu.Unlock()

	go s.eventLoop(ctx)
	go s.refreshLoop(ctx)
	s.refreshWatches()
	slog.Info("watcher started")
	return nil
}

// Stop tears down all watches and timers.
func (s *Service) Stop() {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	s.running = false
	if s.cancel != nil {
		s.cancel()
	}
	if s.fsw != nil {
		_ = s.fsw.Close()
		s.fsw = nil
	}
	for _, t := range s.timers {
		t.Stop()
	}
	s.timers = map[string]*time.Timer{}
	s.watched = map[string]string{}
	s.mu.Unlock()
	slog.Info("watcher stopped")
}

// IsRunning reports the current state.
func (s *Service) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

// refreshWatches syncs fsnotify subscriptions with the current set of
// save_locations in the DB (recursively adds subdirs too).
func (s *Service) refreshWatches() {
	games, err := s.db.ListGames()
	if err != nil {
		return
	}
	want := map[string]string{} // dir → locID
	for _, g := range games {
		locs, _ := s.db.ListSaveLocations(g.ID)
		for _, l := range locs {
			if !l.WatchEnabled {
				continue
			}
			st, err := os.Stat(l.Path)
			if err != nil {
				continue
			}
			if st.IsDir() {
				// add the dir + every subdir
				_ = filepath.WalkDir(l.Path, func(p string, d os.DirEntry, err error) error {
					if err != nil {
						return nil
					}
					if d.IsDir() {
						want[p] = l.ID
					}
					return nil
				})
			} else {
				// watch the file's parent dir; the event filter will narrow
				want[filepath.Dir(l.Path)] = l.ID
			}
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running || s.fsw == nil {
		return
	}
	// Add new dirs.
	for p, id := range want {
		if _, ok := s.watched[p]; ok {
			continue
		}
		if err := s.fsw.Add(p); err != nil {
			continue
		}
		s.watched[p] = id
	}
	// Remove gone dirs.
	for p := range s.watched {
		if _, ok := want[p]; ok {
			continue
		}
		_ = s.fsw.Remove(p)
		delete(s.watched, p)
	}
}

func (s *Service) eventLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-s.fsw.Events:
			if !ok {
				return
			}
			// Ignore CHMOD-only events — they're noisy on Windows and don't
			// represent content change.
			if ev.Op == fsnotify.Chmod {
				continue
			}
			s.handleEvent(ev)
		case err, ok := <-s.fsw.Errors:
			if !ok {
				return
			}
			slog.Warn("watcher fsnotify error", "err", err)
		}
	}
}

func (s *Service) refreshLoop(ctx context.Context) {
	t := time.NewTicker(5 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.refreshWatches()
		}
	}
}

// handleEvent finds the longest matching watched dir for the event path and
// (re)arms its debounce timer.
func (s *Service) handleEvent(ev fsnotify.Event) {
	pathLow := strings.ToLower(ev.Name)
	var locID, bestPath string
	s.mu.Lock()
	for wp, id := range s.watched {
		wpLow := strings.ToLower(wp)
		if pathLow == wpLow || strings.HasPrefix(pathLow, wpLow+string(filepath.Separator)) {
			if len(wp) > len(bestPath) {
				bestPath = wp
				locID = id
			}
		}
	}
	s.mu.Unlock()
	if locID == "" {
		return
	}

	debounce := time.Duration(s.cfg.WatcherDebounceMs) * time.Millisecond
	if debounce < 30*time.Second {
		debounce = 30 * time.Second
	}

	s.mu.Lock()
	if t, ok := s.timers[locID]; ok {
		t.Stop()
	}
	s.timers[locID] = time.AfterFunc(debounce, func() { s.fireBackup(locID) })
	s.mu.Unlock()
}

func (s *Service) fireBackup(locID string) {
	loc, err := s.db.GetSaveLocation(locID)
	if err != nil {
		return
	}
	g, err := s.db.GetGame(loc.GameID)
	if err != nil {
		return
	}
	// If the game is running, defer 60s and re-check.
	if s.isAnyInstallRunning(g.ID) {
		slog.Info("watcher: game running, deferring backup", "game", g.Name)
		s.mu.Lock()
		s.timers[locID] = time.AfterFunc(60*time.Second, func() { s.fireBackup(locID) })
		s.mu.Unlock()
		return
	}
	slog.Info("watcher: auto-backup", "game", g.Name, "path", loc.Path)
	if _, err := s.bk.BackupGame(context.Background(), g.ID, domain.TriggerAuto); err != nil {
		slog.Warn("watcher: backup failed", "game", g.Name, "err", err)
	}
}
