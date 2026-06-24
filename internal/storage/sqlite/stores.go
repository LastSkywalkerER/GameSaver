package sqlite

import (
	"time"

	"GameSaver/internal/domain"
)

// ─── store_accounts ───────────────────────────────────────────────────────

func (s *Store) UpsertStoreAccount(a *domain.StoreAccount) error {
	if a.AddedAt == 0 {
		a.AddedAt = time.Now().Unix()
	}
	_, err := s.DB.Exec(`
		INSERT INTO store_accounts (id,store,external_id,display_name,avatar_url,added_at,last_sync_at,last_sync_error,enabled)
		VALUES(?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET
			display_name=excluded.display_name,
			avatar_url=excluded.avatar_url,
			enabled=excluded.enabled
	`, a.ID, string(a.Store), a.ExternalID, a.DisplayName, a.AvatarURL, a.AddedAt,
		a.LastSyncAt, a.LastSyncError, boolToInt(a.Enabled))
	return err
}

const storeAccountCols = `id,store,external_id,COALESCE(display_name,''),COALESCE(avatar_url,''),
	added_at,COALESCE(last_sync_at,0),COALESCE(last_sync_error,''),enabled`

func scanStoreAccount(rs rowScanner) (*domain.StoreAccount, error) {
	a := &domain.StoreAccount{}
	var store string
	var enabled int
	if err := rs.Scan(&a.ID, &store, &a.ExternalID, &a.DisplayName, &a.AvatarURL,
		&a.AddedAt, &a.LastSyncAt, &a.LastSyncError, &enabled); err != nil {
		return nil, err
	}
	a.Store = domain.SourceKind(store)
	a.Enabled = enabled != 0
	return a, nil
}

func (s *Store) ListStoreAccounts() ([]*domain.StoreAccount, error) {
	rows, err := s.DB.Query(`SELECT ` + storeAccountCols + ` FROM store_accounts ORDER BY store, display_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*domain.StoreAccount{}
	for rows.Next() {
		a, err := scanStoreAccount(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) GetStoreAccount(id string) (*domain.StoreAccount, error) {
	row := s.DB.QueryRow(`SELECT `+storeAccountCols+` FROM store_accounts WHERE id=?`, id)
	a, err := scanStoreAccount(row)
	if err != nil {
		return nil, ErrNotFound
	}
	return a, nil
}

func (s *Store) DeleteStoreAccount(id string) error {
	// owned_titles cascade via the FK ON DELETE CASCADE.
	_, err := s.DB.Exec(`DELETE FROM store_accounts WHERE id=?`, id)
	return err
}

func (s *Store) SetStoreAccountEnabled(id string, enabled bool) error {
	_, err := s.DB.Exec(`UPDATE store_accounts SET enabled=? WHERE id=?`, boolToInt(enabled), id)
	return err
}

func (s *Store) SetAccountSyncStatus(id string, syncAt int64, errMsg string) error {
	_, err := s.DB.Exec(`UPDATE store_accounts SET last_sync_at=?, last_sync_error=? WHERE id=?`,
		syncAt, errMsg, id)
	return err
}

// ─── owned_titles ─────────────────────────────────────────────────────────

func (s *Store) UpsertOwnedTitle(o *domain.OwnedTitle) error {
	now := time.Now().Unix()
	if o.FirstSeenAt == 0 {
		o.FirstSeenAt = now
	}
	o.LastSeenAt = now
	_, err := s.DB.Exec(`
		INSERT INTO owned_titles (id,account_id,store,store_app_id,title,steam_app_id,game_id,playtime_min,icon_url,extra,first_seen_at,last_seen_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(account_id,store_app_id) DO UPDATE SET
			title=excluded.title,
			steam_app_id=excluded.steam_app_id,
			playtime_min=excluded.playtime_min,
			icon_url=excluded.icon_url,
			extra=excluded.extra,
			last_seen_at=excluded.last_seen_at
	`, o.ID, o.AccountID, string(o.Store), o.StoreAppID, o.Title, o.SteamAppID,
		nullStr(o.GameID), o.PlaytimeMin, o.IconURL, o.Extra, o.FirstSeenAt, o.LastSeenAt)
	return err
}

// SetOwnedGameID links an owned title to the canonical game row (merger), or
// clears the link when gameID is "" (NULL — keeps the nullable FK valid).
func (s *Store) SetOwnedGameID(id, gameID string) error {
	_, err := s.DB.Exec(`UPDATE owned_titles SET game_id=? WHERE id=?`, nullStr(gameID), id)
	return err
}

// DeleteOwnedTitlesOlderThan prunes an account's titles not seen in the latest
// sync (no longer owned). `before` is the sync-start timestamp.
func (s *Store) DeleteOwnedTitlesOlderThan(accountID string, before int64) error {
	_, err := s.DB.Exec(`DELETE FROM owned_titles WHERE account_id=? AND last_seen_at < ?`, accountID, before)
	return err
}

const ownedTitleCols = `id,account_id,store,store_app_id,title,COALESCE(steam_app_id,0),
	COALESCE(game_id,''),COALESCE(playtime_min,0),COALESCE(icon_url,''),COALESCE(extra,''),
	first_seen_at,last_seen_at`

func scanOwnedTitle(rs rowScanner) (*domain.OwnedTitle, error) {
	o := &domain.OwnedTitle{}
	var store string
	if err := rs.Scan(&o.ID, &o.AccountID, &store, &o.StoreAppID, &o.Title, &o.SteamAppID,
		&o.GameID, &o.PlaytimeMin, &o.IconURL, &o.Extra, &o.FirstSeenAt, &o.LastSeenAt); err != nil {
		return nil, err
	}
	o.Store = domain.SourceKind(store)
	return o, nil
}

func (s *Store) ListOwnedTitles(accountID string) ([]*domain.OwnedTitle, error) {
	return s.queryOwned(`SELECT `+ownedTitleCols+` FROM owned_titles WHERE account_id=?`, accountID)
}

func (s *Store) ListOwnedTitlesForGame(gameID string) ([]*domain.OwnedTitle, error) {
	return s.queryOwned(`SELECT `+ownedTitleCols+` FROM owned_titles WHERE game_id=?`, gameID)
}

func (s *Store) ListAllOwnedTitles() ([]*domain.OwnedTitle, error) {
	return s.queryOwned(`SELECT ` + ownedTitleCols + ` FROM owned_titles`)
}

func (s *Store) queryOwned(q string, args ...any) ([]*domain.OwnedTitle, error) {
	rows, err := s.DB.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*domain.OwnedTitle{}
	for rows.Next() {
		o, err := scanOwnedTitle(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// nullStr returns nil for an empty string so a nullable FK column (game_id)
// stays NULL rather than referencing a non-existent ''-id row.
func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}
