// Package playtime detects when a known game's executable runs, records start
// + end timestamps as play_sessions, and updates per-game lifetime totals.
// Pure passive polling — works whether the user launched via our Play button
// or directly from Steam/Explorer.
package playtime

import (
	"context"
	"log/slog"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"GameSaver/internal/domain"
	"GameSaver/internal/storage/sqlite"
	"GameSaver/internal/util"
)

// Source values for play_sessions.source.
const (
	SourceAuto   = "auto"
	SourceManual = "manual" // started via our Launch button (set explicitly by caller)
)

// pollInterval is how often we list processes; shorter = more accurate end
// times, longer = less CPU. 30s is a sane default.
const pollInterval = 30 * time.Second

// Service tracks live game sessions and writes them to the DB.
type Service struct {
	db *sqlite.Store

	mu       sync.Mutex
	active   map[string]*liveSession // gameID → session
	cancel   context.CancelFunc
	running  bool
}

type liveSession struct {
	id        string
	startedAt int64
	exeName   string // basename we matched
}

func New(db *sqlite.Store) *Service {
	return &Service{db: db, active: map[string]*liveSession{}}
}

func (s *Service) Start(parent context.Context) {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(parent)
	s.cancel = cancel
	s.running = true
	s.mu.Unlock()
	// Close any sessions left dangling from a crash on prior run.
	_ = s.db.CloseDanglingSessions(time.Now().Unix())
	go s.loop(ctx)
	slog.Info("playtime tracker started")
}

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
	// Persist whatever's currently live so totals aren't lost.
	now := time.Now().Unix()
	for gameID, sess := range s.active {
		_ = s.db.CloseSession(sess.id, now, now-sess.startedAt)
		_ = s.db.UpdateGamePlayStats(gameID, now, now-sess.startedAt)
	}
	s.active = map[string]*liveSession{}
	s.mu.Unlock()
	slog.Info("playtime tracker stopped")
}

func (s *Service) loop(ctx context.Context) {
	// First tick immediately, then on interval.
	s.poll()
	t := time.NewTicker(pollInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.poll()
		}
	}
}

// poll scans running processes and reconciles with our active-session map.
func (s *Service) poll() {
	running := snapshotProcessNames()
	if running == nil {
		return
	}

	games, err := s.db.ListGames()
	if err != nil {
		return
	}

	now := time.Now().Unix()
	seen := map[string]string{} // gameID → exeName basename
	for _, g := range games {
		insts, _ := s.db.ListInstallations(g.ID)
		for _, inst := range insts {
			if inst.ExePath == "" || strings.HasSuffix(inst.ExePath, "_no_exe_") {
				continue
			}
			name := strings.ToLower(filepath.Base(inst.ExePath))
			if running[name] {
				seen[g.ID] = name
				break
			}
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Open new sessions.
	for gameID, exeName := range seen {
		if _, ok := s.active[gameID]; ok {
			continue
		}
		ps := &domain.PlaySession{
			ID:        "sess_" + util.SHA1Hex(gameID+"|"+exeName+"|"+timeToStr(now))[:16],
			GameID:    gameID,
			StartedAt: now,
			Source:    SourceAuto,
		}
		if err := s.db.InsertSession(ps); err != nil {
			slog.Warn("playtime: insert session", "game", gameID, "err", err)
			continue
		}
		s.active[gameID] = &liveSession{id: ps.ID, startedAt: now, exeName: exeName}
		_ = s.db.MarkGamePlaying(gameID, now)
		slog.Info("playtime: session start", "gameId", gameID, "exe", exeName)
	}

	// Close sessions whose exe is no longer running.
	for gameID, sess := range s.active {
		if _, stillRunning := seen[gameID]; stillRunning {
			continue
		}
		dur := now - sess.startedAt
		if dur < 0 {
			dur = 0
		}
		_ = s.db.CloseSession(sess.id, now, dur)
		_ = s.db.UpdateGamePlayStats(gameID, now, dur)
		slog.Info("playtime: session end", "gameId", gameID, "seconds", dur)
		delete(s.active, gameID)
	}
}

func timeToStr(ts int64) string {
	return strings.ReplaceAll(time.Unix(ts, 0).UTC().Format("20060102T150405Z"), "-", "")
}

// snapshotProcessNames returns a lowercased set of running exe basenames via
// tasklist.exe. Returns nil on error so the caller skips the cycle.
func snapshotProcessNames() map[string]bool {
	cmd := exec.Command("tasklist.exe", "/NH", "/FO", "CSV")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	set := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		// CSV: "Image Name","PID","Session Name","Session#","Mem Usage"
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, `"`) {
			continue
		}
		end := strings.Index(line[1:], `"`)
		if end < 0 {
			continue
		}
		name := strings.ToLower(line[1 : 1+end])
		if name != "" {
			set[name] = true
		}
	}
	return set
}
