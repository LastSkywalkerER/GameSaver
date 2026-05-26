package sqlite

import (
	"database/sql"
	"time"
)

// GetManifestOverride returns the manifest key the user pinned for the game,
// or "" if no override exists.
func (s *Store) GetManifestOverride(gameID string) (string, error) {
	row := s.DB.QueryRow(`SELECT COALESCE(manifest_key,'') FROM game_overrides WHERE game_id=?`, gameID)
	var k string
	if err := row.Scan(&k); err != nil {
		if err == sql.ErrNoRows {
			return "", nil
		}
		return "", err
	}
	return k, nil
}

// SetManifestOverride pins a game to a specific Ludusavi manifest entry.
// Pass key="" to clear the override.
func (s *Store) SetManifestOverride(gameID, key string) error {
	if key == "" {
		_, err := s.DB.Exec(`DELETE FROM game_overrides WHERE game_id=?`, gameID)
		return err
	}
	_, err := s.DB.Exec(`
		INSERT INTO game_overrides (game_id, manifest_key, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(game_id) DO UPDATE SET manifest_key=excluded.manifest_key, updated_at=excluded.updated_at`,
		gameID, key, time.Now().Unix())
	return err
}

// DeleteSaveLocation removes a single save location row.
func (s *Store) DeleteSaveLocation(id string) error {
	_, err := s.DB.Exec(`DELETE FROM save_locations WHERE id=?`, id)
	return err
}
