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
		updated_at INTEGER NOT NULL,
		last_played_at INTEGER NOT NULL DEFAULT 0,
		total_play_seconds INTEGER NOT NULL DEFAULT 0
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

	// play_sessions records each detected game run for playtime stats and
	// "recently played" sorting. ended_at is NULL while a session is in flight.
	`CREATE TABLE IF NOT EXISTS play_sessions (
		id TEXT PRIMARY KEY,
		game_id TEXT NOT NULL REFERENCES games(id) ON DELETE CASCADE,
		installation_id TEXT,
		started_at INTEGER NOT NULL,
		ended_at INTEGER,
		duration_seconds INTEGER NOT NULL DEFAULT 0,
		source TEXT NOT NULL DEFAULT 'auto'
	);`,
	`CREATE INDEX IF NOT EXISTS idx_sessions_game ON play_sessions(game_id, started_at DESC);`,

	// store_accounts / owned_titles — online store libraries (#5). One row per
	// connected account; one owned_titles row per game per account (so the same
	// game on two accounts is two rows). game_id links to the canonical games
	// row once the merger resolves it (NULL until then).
	`CREATE TABLE IF NOT EXISTS store_accounts (
		id TEXT PRIMARY KEY,
		store TEXT NOT NULL,
		external_id TEXT NOT NULL,
		display_name TEXT NOT NULL DEFAULT '',
		avatar_url TEXT NOT NULL DEFAULT '',
		added_at INTEGER NOT NULL,
		last_sync_at INTEGER NOT NULL DEFAULT 0,
		last_sync_error TEXT NOT NULL DEFAULT '',
		enabled INTEGER NOT NULL DEFAULT 1,
		UNIQUE(store, external_id)
	);`,
	`CREATE TABLE IF NOT EXISTS owned_titles (
		id TEXT PRIMARY KEY,
		account_id TEXT NOT NULL REFERENCES store_accounts(id) ON DELETE CASCADE,
		store TEXT NOT NULL,
		store_app_id TEXT NOT NULL,
		title TEXT NOT NULL,
		steam_app_id INTEGER NOT NULL DEFAULT 0,
		game_id TEXT,
		playtime_min INTEGER NOT NULL DEFAULT 0,
		icon_url TEXT NOT NULL DEFAULT '',
		extra TEXT NOT NULL DEFAULT '',
		first_seen_at INTEGER NOT NULL,
		last_seen_at INTEGER NOT NULL,
		UNIQUE(account_id, store_app_id)
	);`,
	`CREATE INDEX IF NOT EXISTS idx_owned_account ON owned_titles(account_id);`,
	`CREATE INDEX IF NOT EXISTS idx_owned_game ON owned_titles(game_id);`,
	`CREATE INDEX IF NOT EXISTS idx_owned_steam ON owned_titles(steam_app_id);`,
}

// extraColumns is run AFTER the main statements to retrofit columns onto an
// existing DB whose tables predate them. Each entry is added only if
// missing — safe to keep here forever.
var extraColumns = []struct{ table, col, def string }{
	{"games", "last_played_at", "INTEGER NOT NULL DEFAULT 0"},
	{"games", "total_play_seconds", "INTEGER NOT NULL DEFAULT 0"},
	// Total on-disk size of the installation root, computed lazily after scan
	// (separate from size_bytes which holds the launch exe's own size).
	{"installations", "install_dir_size_bytes", "INTEGER NOT NULL DEFAULT 0"},
	{"installations", "install_dir_size_at", "INTEGER NOT NULL DEFAULT 0"},
	// Animated hero used as the shell-mode background (webm/mp4 cached
	// under coversDir). Empty = no video, frontend falls back to hero/cover.
	{"games", "video_path", "TEXT NOT NULL DEFAULT ''"},
}

func applyMigrations(db *sql.DB) error {
	for _, stmt := range migrations {
		if _, err := db.Exec(stmt); err != nil {
			return err
		}
	}
	for _, c := range extraColumns {
		if err := addColumnIfMissing(db, c.table, c.col, c.def); err != nil {
			return err
		}
	}
	return nil
}

// addColumnIfMissing performs an idempotent ALTER TABLE ADD COLUMN.
func addColumnIfMissing(db *sql.DB, table, col, def string) error {
	rows, err := db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt any
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return err
		}
		if name == col {
			return nil
		}
	}
	_, err = db.Exec("ALTER TABLE " + table + " ADD COLUMN " + col + " " + def)
	return err
}
