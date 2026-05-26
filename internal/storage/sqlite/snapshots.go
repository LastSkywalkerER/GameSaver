package sqlite

import (
	"database/sql"

	"GameSaver/internal/domain"
)

func (s *Store) InsertSnapshot(sn *domain.Snapshot) error {
	_, err := s.DB.Exec(`
		INSERT INTO snapshots (id,game_id,location_id,archive_path,created_at,trigger,total_bytes,compressed_bytes,file_count,encrypted,content_hash,game_version,note)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		sn.ID, sn.GameID, sn.LocationID, sn.ArchivePath, sn.CreatedAt, string(sn.Trigger),
		sn.TotalBytes, sn.CompressedBytes, sn.FileCount, boolToInt(sn.Encrypted),
		sn.ContentHash, sn.GameVersion, sn.Note)
	return err
}

func (s *Store) DeleteSnapshot(id string) error {
	_, err := s.DB.Exec(`DELETE FROM snapshots WHERE id=?`, id)
	return err
}

func (s *Store) GetSnapshot(id string) (*domain.Snapshot, error) {
	row := s.DB.QueryRow(`SELECT id,game_id,location_id,archive_path,created_at,trigger,total_bytes,compressed_bytes,file_count,encrypted,content_hash,COALESCE(game_version,''),COALESCE(note,'')
		FROM snapshots WHERE id=?`, id)
	sn, err := scanSnapshot(row)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	return sn, err
}

func (s *Store) ListSnapshots(gameID string) ([]*domain.Snapshot, error) {
	rows, err := s.DB.Query(`SELECT id,game_id,location_id,archive_path,created_at,trigger,total_bytes,compressed_bytes,file_count,encrypted,content_hash,COALESCE(game_version,''),COALESCE(note,'')
		FROM snapshots WHERE game_id=? ORDER BY created_at DESC`, gameID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*domain.Snapshot{}
	for rows.Next() {
		sn, err := scanSnapshot(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sn)
	}
	return out, rows.Err()
}

func (s *Store) LatestSnapshotHash(gameID, locationID string) (string, error) {
	row := s.DB.QueryRow(`SELECT content_hash FROM snapshots WHERE game_id=? AND location_id=? ORDER BY created_at DESC LIMIT 1`, gameID, locationID)
	var h string
	if err := row.Scan(&h); err != nil {
		if err == sql.ErrNoRows {
			return "", nil
		}
		return "", err
	}
	return h, nil
}

// CountSnapshots returns number of snapshots for a location.
func (s *Store) CountSnapshots(locationID string) (int, error) {
	row := s.DB.QueryRow(`SELECT COUNT(*) FROM snapshots WHERE location_id=?`, locationID)
	var n int
	err := row.Scan(&n)
	return n, err
}

// OldestSnapshots returns up to `n` oldest snapshots for a location.
func (s *Store) OldestSnapshots(locationID string, n int) ([]*domain.Snapshot, error) {
	rows, err := s.DB.Query(`SELECT id,game_id,location_id,archive_path,created_at,trigger,total_bytes,compressed_bytes,file_count,encrypted,content_hash,COALESCE(game_version,''),COALESCE(note,'')
		FROM snapshots WHERE location_id=? ORDER BY created_at ASC LIMIT ?`, locationID, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*domain.Snapshot{}
	for rows.Next() {
		sn, err := scanSnapshot(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sn)
	}
	return out, rows.Err()
}

func scanSnapshot(rs rowScanner) (*domain.Snapshot, error) {
	sn := &domain.Snapshot{}
	var trig string
	var enc int
	if err := rs.Scan(&sn.ID, &sn.GameID, &sn.LocationID, &sn.ArchivePath, &sn.CreatedAt, &trig,
		&sn.TotalBytes, &sn.CompressedBytes, &sn.FileCount, &enc,
		&sn.ContentHash, &sn.GameVersion, &sn.Note); err != nil {
		return nil, err
	}
	sn.Trigger = domain.Trigger(trig)
	sn.Encrypted = enc != 0
	return sn, nil
}
