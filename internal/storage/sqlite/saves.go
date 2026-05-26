package sqlite

import (
	"database/sql"

	"GameSaver/internal/domain"
)

func (s *Store) UpsertSaveLocation(l *domain.SaveLocation) error {
	_, err := s.DB.Exec(`
		INSERT INTO save_locations (id,game_id,path,kind,source_hint,size_bytes,file_count,mtime,watch_enabled,is_junction,junction_target)
		VALUES(?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET
			path=excluded.path,
			kind=excluded.kind,
			source_hint=excluded.source_hint,
			size_bytes=excluded.size_bytes,
			file_count=excluded.file_count,
			mtime=excluded.mtime,
			watch_enabled=excluded.watch_enabled,
			is_junction=excluded.is_junction,
			junction_target=excluded.junction_target
	`, l.ID, l.GameID, l.Path, string(l.Kind), l.SourceHint, l.SizeBytes, l.FileCount, l.Mtime, boolToInt(l.WatchEnabled), boolToInt(l.IsJunction), l.JunctionTarget)
	return err
}

func (s *Store) ListSaveLocations(gameID string) ([]*domain.SaveLocation, error) {
	rows, err := s.DB.Query(`SELECT id,game_id,path,kind,COALESCE(source_hint,''),size_bytes,file_count,mtime,watch_enabled,is_junction,COALESCE(junction_target,'')
		FROM save_locations WHERE game_id=? ORDER BY path`, gameID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*domain.SaveLocation{}
	for rows.Next() {
		l := &domain.SaveLocation{}
		var kind string
		var watch, junc int
		if err := rows.Scan(&l.ID, &l.GameID, &l.Path, &kind, &l.SourceHint, &l.SizeBytes, &l.FileCount, &l.Mtime, &watch, &junc, &l.JunctionTarget); err != nil {
			return nil, err
		}
		l.Kind = domain.SaveKind(kind)
		l.WatchEnabled = watch != 0
		l.IsJunction = junc != 0
		out = append(out, l)
	}
	return out, rows.Err()
}

func (s *Store) GetSaveLocation(id string) (*domain.SaveLocation, error) {
	row := s.DB.QueryRow(`SELECT id,game_id,path,kind,COALESCE(source_hint,''),size_bytes,file_count,mtime,watch_enabled,is_junction,COALESCE(junction_target,'')
		FROM save_locations WHERE id=?`, id)
	l := &domain.SaveLocation{}
	var kind string
	var watch, junc int
	if err := row.Scan(&l.ID, &l.GameID, &l.Path, &kind, &l.SourceHint, &l.SizeBytes, &l.FileCount, &l.Mtime, &watch, &junc, &l.JunctionTarget); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	l.Kind = domain.SaveKind(kind)
	l.WatchEnabled = watch != 0
	l.IsJunction = junc != 0
	return l, nil
}
