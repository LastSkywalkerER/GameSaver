package sqlite

import (
	"database/sql"

	"GameSaver/internal/domain"
)

// InsertSession creates a new play_session row (with ended_at = NULL).
func (s *Store) InsertSession(ps *domain.PlaySession) error {
	_, err := s.DB.Exec(`
		INSERT INTO play_sessions (id, game_id, installation_id, started_at, ended_at, duration_seconds, source)
		VALUES (?, ?, ?, ?, NULL, 0, ?)`,
		ps.ID, ps.GameID, ps.InstallationID, ps.StartedAt, ps.Source)
	return err
}

// CloseSession sets ended_at + duration_seconds on a session.
func (s *Store) CloseSession(id string, endedAt, duration int64) error {
	_, err := s.DB.Exec(`UPDATE play_sessions SET ended_at=?, duration_seconds=? WHERE id=?`,
		endedAt, duration, id)
	return err
}

// ListSessions returns sessions for a game, newest first.
func (s *Store) ListSessions(gameID string, limit int) ([]*domain.PlaySession, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.DB.Query(`SELECT id, game_id, COALESCE(installation_id,''), started_at,
		COALESCE(ended_at,0), COALESCE(duration_seconds,0), COALESCE(source,'')
		FROM play_sessions WHERE game_id=? ORDER BY started_at DESC LIMIT ?`, gameID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*domain.PlaySession{}
	for rows.Next() {
		ps := &domain.PlaySession{}
		if err := rows.Scan(&ps.ID, &ps.GameID, &ps.InstallationID, &ps.StartedAt, &ps.EndedAt, &ps.DurationSeconds, &ps.Source); err != nil {
			return nil, err
		}
		out = append(out, ps)
	}
	return out, rows.Err()
}

// CloseDanglingSessions ends any session that still has ended_at = NULL
// (e.g. process crash, OS reboot during a session). Called on app startup
// so playtime stats stay sane; uses session start + lookback cap of 4h.
func (s *Store) CloseDanglingSessions(now int64) error {
	rows, err := s.DB.Query(`SELECT id, started_at FROM play_sessions WHERE ended_at IS NULL`)
	if err != nil {
		return err
	}
	defer rows.Close()
	type rec struct {
		id        string
		startedAt int64
	}
	open := []rec{}
	for rows.Next() {
		var r rec
		if err := rows.Scan(&r.id, &r.startedAt); err != nil {
			return err
		}
		open = append(open, r)
	}
	rows.Close()
	const maxLookbackSec = 4 * 60 * 60 // cap at 4h to avoid wild totals
	for _, r := range open {
		dur := now - r.startedAt
		if dur < 0 {
			dur = 0
		}
		if dur > maxLookbackSec {
			dur = maxLookbackSec
		}
		if _, err := s.DB.Exec(`UPDATE play_sessions SET ended_at=?, duration_seconds=? WHERE id=?`,
			r.startedAt+dur, dur, r.id); err != nil {
			return err
		}
	}
	return nil
}

// (Compile-time evidence ErrNotFound stays exported.)
var _ = sql.ErrNoRows
