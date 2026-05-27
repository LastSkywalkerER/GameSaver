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

// UpdateInstallDirSize stores the recursively-computed total size for an
// installation root. Called by the background size-walker after a scan.
func (s *Store) UpdateInstallDirSize(id string, size int64) error {
	_, err := s.DB.Exec(`UPDATE installations
		SET install_dir_size_bytes=?, install_dir_size_at=?
		WHERE id=?`,
		size, time.Now().Unix(), id)
	return err
}

const installSelectCols = `id,game_id,source,COALESCE(source_app_id,''),root_path,exe_path,
		COALESCE(size_bytes,0),last_seen_at,COALESCE(launch_uri,''),
		COALESCE(install_dir_size_bytes,0),COALESCE(install_dir_size_at,0)`

func scanInstallation(rs rowScanner) (*domain.Installation, error) {
	i := &domain.Installation{}
	var src string
	if err := rs.Scan(&i.ID, &i.GameID, &src, &i.SourceAppID, &i.RootPath, &i.ExePath,
		&i.SizeBytes, &i.LastSeenAt, &i.LaunchURI,
		&i.InstallDirSizeBytes, &i.InstallDirSizeAt); err != nil {
		return nil, err
	}
	i.Source = domain.SourceKind(src)
	return i, nil
}

func (s *Store) ListInstallations(gameID string) ([]*domain.Installation, error) {
	rows, err := s.DB.Query(`SELECT `+installSelectCols+`
		FROM installations WHERE game_id=? ORDER BY source, root_path`, gameID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*domain.Installation{}
	for rows.Next() {
		i, err := scanInstallation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

func (s *Store) GetInstallation(id string) (*domain.Installation, error) {
	row := s.DB.QueryRow(`SELECT `+installSelectCols+` FROM installations WHERE id=?`, id)
	i, err := scanInstallation(row)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	return i, err
}

// ListAllInstallations returns every installation across every game. Used by
// the background dir-size computer.
func (s *Store) ListAllInstallations() ([]*domain.Installation, error) {
	rows, err := s.DB.Query(`SELECT ` + installSelectCols + ` FROM installations`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*domain.Installation{}
	for rows.Next() {
		i, err := scanInstallation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}
