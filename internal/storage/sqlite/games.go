package sqlite

import (
	"database/sql"
	"time"

	"GameSaver/internal/domain"
)

// UpsertGame inserts or updates a Game by ID, preserving created_at on update.
// Playtime fields (last_played_at, total_play_seconds) are kept as-is on
// conflict — only the playtime package updates them.
func (s *Store) UpsertGame(g *domain.Game) error {
	now := time.Now().Unix()
	if g.CreatedAt == 0 {
		g.CreatedAt = now
	}
	g.UpdatedAt = now
	_, err := s.DB.Exec(`
		INSERT INTO games (id,name,slug,igdb_id,steam_app_id,cover_path,hero_path,icon_path,genres,release_year,hidden,created_at,updated_at,last_played_at,total_play_seconds)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET
			name=excluded.name,
			slug=excluded.slug,
			igdb_id=COALESCE(NULLIF(excluded.igdb_id,0), games.igdb_id),
			steam_app_id=COALESCE(NULLIF(excluded.steam_app_id,0), games.steam_app_id),
			cover_path=COALESCE(NULLIF(excluded.cover_path,''), games.cover_path),
			hero_path=COALESCE(NULLIF(excluded.hero_path,''), games.hero_path),
			icon_path=COALESCE(NULLIF(excluded.icon_path,''), games.icon_path),
			genres=COALESCE(NULLIF(excluded.genres,''), games.genres),
			release_year=COALESCE(NULLIF(excluded.release_year,0), games.release_year),
			hidden=excluded.hidden,
			updated_at=excluded.updated_at
	`, g.ID, g.Name, g.Slug, g.IGDBID, g.SteamAppID, g.CoverPath, g.HeroPath, g.IconPath, g.Genres, g.ReleaseYear, boolToInt(g.Hidden), g.CreatedAt, g.UpdatedAt, g.LastPlayedAt, g.TotalPlaySeconds)
	return err
}

const gameSelectCols = `id,name,slug,COALESCE(igdb_id,0),COALESCE(steam_app_id,0),
		COALESCE(cover_path,''),COALESCE(hero_path,''),COALESCE(icon_path,''),
		COALESCE(genres,''),COALESCE(release_year,0),hidden,created_at,updated_at,
		COALESCE(last_played_at,0),COALESCE(total_play_seconds,0)`

func (s *Store) GetGame(id string) (*domain.Game, error) {
	row := s.DB.QueryRow(`SELECT `+gameSelectCols+` FROM games WHERE id=?`, id)
	g, err := scanGame(row)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	return g, err
}

// FindGameBySteamAppID returns the game with the given Steam AppID (or ErrNotFound).
func (s *Store) FindGameBySteamAppID(appID int64) (*domain.Game, error) {
	row := s.DB.QueryRow(`SELECT `+gameSelectCols+` FROM games WHERE steam_app_id=? LIMIT 1`, appID)
	g, err := scanGame(row)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	return g, err
}

// FindGameBySlug returns a Game with the given slug.
func (s *Store) FindGameBySlug(slug string) (*domain.Game, error) {
	row := s.DB.QueryRow(`SELECT `+gameSelectCols+` FROM games WHERE slug=? LIMIT 1`, slug)
	g, err := scanGame(row)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	return g, err
}

func (s *Store) ListGames() ([]*domain.Game, error) {
	rows, err := s.DB.Query(`SELECT ` + gameSelectCols + ` FROM games ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*domain.Game{}
	for rows.Next() {
		g, err := scanGame(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func (s *Store) SetGameHidden(id string, hidden bool) error {
	_, err := s.DB.Exec(`UPDATE games SET hidden=?, updated_at=? WHERE id=?`,
		boolToInt(hidden), time.Now().Unix(), id)
	return err
}

func (s *Store) UpdateGameCovers(id, cover, hero, icon string) error {
	_, err := s.DB.Exec(`UPDATE games SET
		cover_path=COALESCE(NULLIF(?,''), cover_path),
		hero_path=COALESCE(NULLIF(?,''), hero_path),
		icon_path=COALESCE(NULLIF(?,''), icon_path),
		updated_at=? WHERE id=?`,
		cover, hero, icon, time.Now().Unix(), id)
	return err
}

// UpdateGamePlayStats bumps last_played_at and adds delta seconds to
// total_play_seconds. Called by the playtime tracker at session end.
func (s *Store) UpdateGamePlayStats(id string, lastPlayedAt, addSeconds int64) error {
	_, err := s.DB.Exec(`UPDATE games SET
		last_played_at=MAX(COALESCE(last_played_at,0), ?),
		total_play_seconds=COALESCE(total_play_seconds,0) + ?,
		updated_at=? WHERE id=?`,
		lastPlayedAt, addSeconds, time.Now().Unix(), id)
	return err
}

// MarkGamePlaying just bumps last_played_at — used when a session starts so
// "Recently played" sort reflects active sessions immediately.
func (s *Store) MarkGamePlaying(id string, ts int64) error {
	_, err := s.DB.Exec(`UPDATE games SET
		last_played_at=MAX(COALESCE(last_played_at,0), ?),
		updated_at=? WHERE id=?`,
		ts, time.Now().Unix(), id)
	return err
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanGame(rs rowScanner) (*domain.Game, error) {
	g := &domain.Game{}
	var hidden int
	if err := rs.Scan(&g.ID, &g.Name, &g.Slug, &g.IGDBID, &g.SteamAppID,
		&g.CoverPath, &g.HeroPath, &g.IconPath,
		&g.Genres, &g.ReleaseYear, &hidden, &g.CreatedAt, &g.UpdatedAt,
		&g.LastPlayedAt, &g.TotalPlaySeconds); err != nil {
		return nil, err
	}
	g.Hidden = hidden != 0
	return g, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
