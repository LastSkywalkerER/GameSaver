package sqlite

import (
	"database/sql"
	"time"

	"GameSaver/internal/domain"
)

// UpsertGame inserts or updates a Game by ID, preserving created_at on update.
func (s *Store) UpsertGame(g *domain.Game) error {
	now := time.Now().Unix()
	if g.CreatedAt == 0 {
		g.CreatedAt = now
	}
	g.UpdatedAt = now
	_, err := s.DB.Exec(`
		INSERT INTO games (id,name,slug,igdb_id,steam_app_id,cover_path,hero_path,icon_path,genres,release_year,hidden,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)
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
	`, g.ID, g.Name, g.Slug, g.IGDBID, g.SteamAppID, g.CoverPath, g.HeroPath, g.IconPath, g.Genres, g.ReleaseYear, boolToInt(g.Hidden), g.CreatedAt, g.UpdatedAt)
	return err
}

func (s *Store) GetGame(id string) (*domain.Game, error) {
	row := s.DB.QueryRow(`SELECT id,name,slug,COALESCE(igdb_id,0),COALESCE(steam_app_id,0),
		COALESCE(cover_path,''),COALESCE(hero_path,''),COALESCE(icon_path,''),
		COALESCE(genres,''),COALESCE(release_year,0),hidden,created_at,updated_at FROM games WHERE id=?`, id)
	g, err := scanGame(row)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	return g, err
}

// FindGameBySteamAppID returns the game with the given Steam AppID (or ErrNotFound).
func (s *Store) FindGameBySteamAppID(appID int64) (*domain.Game, error) {
	row := s.DB.QueryRow(`SELECT id,name,slug,COALESCE(igdb_id,0),COALESCE(steam_app_id,0),
		COALESCE(cover_path,''),COALESCE(hero_path,''),COALESCE(icon_path,''),
		COALESCE(genres,''),COALESCE(release_year,0),hidden,created_at,updated_at FROM games WHERE steam_app_id=? LIMIT 1`, appID)
	g, err := scanGame(row)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	return g, err
}

// FindGameBySlug returns a Game with the given slug.
func (s *Store) FindGameBySlug(slug string) (*domain.Game, error) {
	row := s.DB.QueryRow(`SELECT id,name,slug,COALESCE(igdb_id,0),COALESCE(steam_app_id,0),
		COALESCE(cover_path,''),COALESCE(hero_path,''),COALESCE(icon_path,''),
		COALESCE(genres,''),COALESCE(release_year,0),hidden,created_at,updated_at FROM games WHERE slug=? LIMIT 1`, slug)
	g, err := scanGame(row)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	return g, err
}

func (s *Store) ListGames() ([]*domain.Game, error) {
	rows, err := s.DB.Query(`SELECT id,name,slug,COALESCE(igdb_id,0),COALESCE(steam_app_id,0),
		COALESCE(cover_path,''),COALESCE(hero_path,''),COALESCE(icon_path,''),
		COALESCE(genres,''),COALESCE(release_year,0),hidden,created_at,updated_at FROM games ORDER BY name`)
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

type rowScanner interface {
	Scan(dest ...any) error
}

func scanGame(rs rowScanner) (*domain.Game, error) {
	g := &domain.Game{}
	var hidden int
	if err := rs.Scan(&g.ID, &g.Name, &g.Slug, &g.IGDBID, &g.SteamAppID, &g.CoverPath, &g.HeroPath, &g.IconPath, &g.Genres, &g.ReleaseYear, &hidden, &g.CreatedAt, &g.UpdatedAt); err != nil {
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
