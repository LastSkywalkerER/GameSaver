// Package dirsize computes installation directory sizes asynchronously so the
// initial scan stays fast (~5s) while users see cards immediately. Sizes
// trickle into the DB over the following minutes and the UI re-renders on
// every "inst:size" event.
package dirsize

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"GameSaver/internal/storage/sqlite"
	"GameSaver/internal/util"
)

// staleAfter — recompute a dir-size if older than this.
const staleAfter = 24 * time.Hour

// Service wraps the periodic background size-walker.
type Service struct {
	db   *sqlite.Store
	emit func(string, any)
}

func New(db *sqlite.Store, emit func(string, any)) *Service {
	return &Service{db: db, emit: emit}
}

// Run walks every installation root, computes its on-disk size, persists the
// value, and emits "inst:size" {id, gameId, size} after each one. Returns
// when all installs have been processed or ctx is cancelled. Cheap to call
// repeatedly: rows newer than `staleAfter` are skipped unless `force=true`.
func (s *Service) Run(ctx context.Context, force bool) {
	insts, err := s.db.ListAllInstallations()
	if err != nil {
		return
	}
	now := time.Now().Unix()
	cutoff := now - int64(staleAfter.Seconds())
	for _, inst := range insts {
		if ctx.Err() != nil {
			return
		}
		if !force && inst.InstallDirSizeAt > cutoff && inst.InstallDirSizeBytes > 0 {
			continue
		}
		if inst.RootPath == "" {
			continue
		}
		// Skip placeholder Minecraft installations whose RootPath is a
		// launcher dir rather than a game install (no real "size").
		if strings.HasSuffix(strings.ToLower(inst.ExePath), "_no_exe_") {
			continue
		}
		size, _, _ := util.DirSizeAndCount(inst.RootPath)
		if err := s.db.UpdateInstallDirSize(inst.ID, size); err != nil {
			slog.Warn("dirsize: update", "id", inst.ID, "err", err)
			continue
		}
		if s.emit != nil {
			s.emit("inst:size", map[string]any{
				"id":     inst.ID,
				"gameId": inst.GameID,
				"size":   size,
			})
		}
	}
}
