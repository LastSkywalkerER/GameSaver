package sqlite

import (
	"database/sql"
	"time"

	"GameSaver/internal/domain"
)

func (s *Store) UpsertInstallation(i *domain.Installation) error {
	if i.LastSeenAt == 0 {
		i.LastSeenAt = time.Now().Unix()
	}
	_, err := s.DB.Exec(`
		INSERT INTO installations (id,game_id,source,source_app_id,root_path,exe_path,size_bytes,last_seen_at,launch_uri)
		VALUES(?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET
			game_id=excluded.game_id,
			source=excluded.source,
			source_app_id=excluded.source_app_id,
			root_path=excluded.root_path,
			exe_path=excluded.exe_path,
			size_bytes=excluded.size_bytes,
			last_seen_at=excluded.last_seen_at,
			launch_uri=excluded.launch_uri
	`, i.ID, i.GameID, string(i.Source), i.SourceAppID, i.RootPath, i.ExePath, i.SizeBytes, i.LastSeenAt, i.LaunchURI)
	return err
}

func (s *Store) ListInstallations(gameID string) ([]*domain.Installation, error) {
	rows, err := s.DB.Query(`SELECT id,game_id,source,COALESCE(source_app_id,''),root_path,exe_path,COALESCE(size_bytes,0),last_seen_at,COALESCE(launch_uri,'')
		FROM installations WHERE game_id=? ORDER BY source, root_path`, gameID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*domain.Installation{}
	for rows.Next() {
		i := &domain.Installation{}
		var src string
		if err := rows.Scan(&i.ID, &i.GameID, &src, &i.SourceAppID, &i.RootPath, &i.ExePath, &i.SizeBytes, &i.LastSeenAt, &i.LaunchURI); err != nil {
			return nil, err
		}
		i.Source = domain.SourceKind(src)
		out = append(out, i)
	}
	return out, rows.Err()
}

func (s *Store) GetInstallation(id string) (*domain.Installation, error) {
	row := s.DB.QueryRow(`SELECT id,game_id,source,COALESCE(source_app_id,''),root_path,exe_path,COALESCE(size_bytes,0),last_seen_at,COALESCE(launch_uri,'')
		FROM installations WHERE id=?`, id)
	i := &domain.Installation{}
	var src string
	if err := row.Scan(&i.ID, &i.GameID, &src, &i.SourceAppID, &i.RootPath, &i.ExePath, &i.SizeBytes, &i.LastSeenAt, &i.LaunchURI); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	i.Source = domain.SourceKind(src)
	return i, nil
}
