package sqlite

import "database/sql"

var migrations = []string{
	`CREATE TABLE IF NOT EXISTS schema_version (version INTEGER PRIMARY KEY);`,
	`CREATE TABLE IF NOT EXISTS games (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		slug TEXT NOT NULL UNIQUE,
		igdb_id INTEGER,
		steam_app_id INTEGER,
		cover_path TEXT,
		hero_path TEXT,
		icon_path TEXT,
		genres TEXT,
		release_year INTEGER,
		hidden INTEGER NOT NULL DEFAULT 0,
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL
	);`,
	`CREATE INDEX IF NOT EXISTS idx_games_name ON games(name);`,
	`CREATE INDEX IF NOT EXISTS idx_games_appid ON games(steam_app_id);`,

	`CREATE TABLE IF NOT EXISTS installations (
		id TEXT PRIMARY KEY,
		game_id TEXT NOT NULL REFERENCES games(id) ON DELETE CASCADE,
		source TEXT NOT NULL,
		source_app_id TEXT,
		root_path TEXT NOT NULL,
		exe_path TEXT NOT NULL,
		size_bytes INTEGER,
		last_seen_at INTEGER NOT NULL,
		launch_uri TEXT,
		UNIQUE(game_id, root_path)
	);`,
	`CREATE INDEX IF NOT EXISTS idx_inst_game ON installations(game_id);`,

	`CREATE TABLE IF NOT EXISTS save_locations (
		id TEXT PRIMARY KEY,
		game_id TEXT NOT NULL REFERENCES games(id) ON DELETE CASCADE,
		path TEXT NOT NULL,
		kind TEXT NOT NULL,
		source_hint TEXT,
		size_bytes INTEGER NOT NULL DEFAULT 0,
		file_count INTEGER NOT NULL DEFAULT 0,
		mtime INTEGER NOT NULL DEFAULT 0,
		watch_enabled INTEGER NOT NULL DEFAULT 1,
		is_junction INTEGER NOT NULL DEFAULT 0,
		junction_target TEXT,
		UNIQUE(game_id, path)
	);`,
	`CREATE INDEX IF NOT EXISTS idx_save_game ON save_locations(game_id);`,

	`CREATE TABLE IF NOT EXISTS snapshots (
		id TEXT PRIMARY KEY,
		game_id TEXT NOT NULL REFERENCES games(id) ON DELETE CASCADE,
		location_id TEXT NOT NULL REFERENCES save_locations(id) ON DELETE CASCADE,
		archive_path TEXT NOT NULL,
		created_at INTEGER NOT NULL,
		trigger TEXT NOT NULL,
		total_bytes INTEGER NOT NULL,
		compressed_bytes INTEGER NOT NULL,
		file_count INTEGER NOT NULL,
		encrypted INTEGER NOT NULL DEFAULT 0,
		content_hash TEXT NOT NULL,
		game_version TEXT,
		note TEXT
	);`,
	`CREATE INDEX IF NOT EXISTS idx_snap_game_loc ON snapshots(game_id, location_id, created_at DESC);`,

	`CREATE TABLE IF NOT EXISTS scan_state (
		path TEXT PRIMARY KEY,
		mtime INTEGER NOT NULL,
		size INTEGER NOT NULL,
		fingerprint TEXT NOT NULL,
		last_scanned INTEGER NOT NULL
	);`,

	// game_overrides stores user-supplied corrections that survive re-scans:
	//  - manifest_key: pin this game to a specific Ludusavi entry name
	//  - hidden: opt-out of automatic match (don't pick anything)
	`CREATE TABLE IF NOT EXISTS game_overrides (
		game_id TEXT PRIMARY KEY REFERENCES games(id) ON DELETE CASCADE,
		manifest_key TEXT,
		updated_at INTEGER NOT NULL
	);`,
}

func applyMigrations(db *sql.DB) error {
	for _, stmt := range migrations {
		if _, err := db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}
