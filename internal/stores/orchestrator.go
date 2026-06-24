package stores

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"GameSaver/internal/domain"
	"GameSaver/internal/match"
	"GameSaver/internal/secrets"
	"GameSaver/internal/storage/sqlite"
	"GameSaver/internal/util"
)

// Service orchestrates the store providers: add/sync accounts, persist owned
// titles, merge into the canonical games, and build the aggregated library.
type Service struct {
	db    *sqlite.Store
	vault *secrets.Vault
	emit  func(string, any)
	hc    *http.Client
	provs map[domain.SourceKind]Provider
	mu    sync.Mutex // serialises syncs (and the stub-game creation they do)
}

func New(db *sqlite.Store, vault *secrets.Vault, emit func(string, any)) *Service {
	hc := &http.Client{Timeout: 30 * time.Second}
	s := &Service{db: db, vault: vault, emit: emit, hc: hc, provs: map[domain.SourceKind]Provider{}}
	s.provs[domain.SourceSteam] = &steamProvider{hc: hc}
	s.provs[domain.SourceGOG] = &gogProvider{hc: hc}
	s.provs[domain.SourceEpic] = &epicProvider{hc: hc}
	s.provs[domain.SourceEA] = &eaProvider{hc: hc}
	return s
}

func (s *Service) emitEv(ev string, p any) {
	if s.emit != nil {
		s.emit(ev, p)
	}
}

// LoginInfo tells the UI how to connect an account for a store.
type LoginInfo struct {
	Store       domain.SourceKind `json:"store"`
	URL         string            `json:"url"`
	Hint        string            `json:"hint"`
	Interactive bool              `json:"interactive"`
}

func (s *Service) LoginInfo(store domain.SourceKind) LoginInfo {
	li := LoginInfo{Store: store}
	if p, ok := s.provs[store]; ok {
		li.URL, li.Hint = p.LoginURL()
		li.Interactive = li.URL != ""
	}
	return li
}

// AddAccount connects a new account, persists it, and kicks off a first sync.
func (s *Service) AddAccount(ctx context.Context, store domain.SourceKind, in AddAccountInput) (*domain.StoreAccount, error) {
	p, ok := s.provs[store]
	if !ok {
		return nil, fmt.Errorf("неизвестный магазин %q", store)
	}
	info, err := p.AddAccount(ctx, in, s.vault)
	if err != nil {
		return nil, err
	}
	acct := &domain.StoreAccount{
		ID:          "acct_" + util.SHA1Hex(string(store)+"|"+info.ExternalID)[:16],
		Store:       store,
		ExternalID:  info.ExternalID,
		DisplayName: info.DisplayName,
		AvatarURL:   info.AvatarURL,
		AddedAt:     time.Now().Unix(),
		Enabled:     true,
	}
	if err := s.db.UpsertStoreAccount(acct); err != nil {
		return nil, err
	}
	go func() {
		_ = s.SyncAccount(context.Background(), acct.ID)
		s.emitEv("stores:library-changed", nil)
	}()
	return acct, nil
}

// SetSteamKey stores (or clears) the shared Steam Web API key — the reliable
// fallback when the keyless community endpoint won't return the library.
func (s *Service) SetSteamKey(key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return s.vault.Delete(string(domain.SourceSteam), "*")
	}
	return saveTokens(s.vault, domain.SourceSteam, "*", tokenBundle{APIKey: key})
}

// AddSteamViaOpenID runs the browser "Sign in through Steam" flow and adds the
// resulting account (keyless — owned games come from the public profile).
// openBrowser is invoked with the Steam login URL.
func (s *Service) AddSteamViaOpenID(ctx context.Context, openBrowser func(string)) (*domain.StoreAccount, error) {
	steamID, err := steamOpenIDLogin(ctx, s.hc, openBrowser)
	if err != nil {
		return nil, err
	}
	return s.AddAccount(ctx, domain.SourceSteam, AddAccountInput{SteamRef: steamID})
}

// SyncAccount fetches an account's owned titles, upserts + prunes them, then
// re-merges. Records last_sync_error on failure but keeps prior titles.
func (s *Service) SyncAccount(ctx context.Context, accountID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	acct, err := s.db.GetStoreAccount(accountID)
	if err != nil {
		return err
	}
	p, ok := s.provs[acct.Store]
	if !ok {
		return fmt.Errorf("нет провайдера для %q", acct.Store)
	}
	start := time.Now().Unix()
	s.emitEv("stores:sync-start", map[string]any{"accountId": accountID})

	titles, ferr := p.FetchOwned(ctx, acct, s.vault)
	if ferr != nil {
		_ = s.db.SetAccountSyncStatus(accountID, start, ferr.Error())
		s.emitEv("stores:account-done", map[string]any{"accountId": accountID, "error": ferr.Error()})
		return ferr
	}
	for _, t := range titles {
		ot := &domain.OwnedTitle{
			ID:          "own_" + util.SHA1Hex(accountID+"|"+t.StoreAppID)[:16],
			AccountID:   accountID,
			Store:       acct.Store,
			StoreAppID:  t.StoreAppID,
			Title:       t.Title,
			SteamAppID:  t.SteamAppID,
			PlaytimeMin: t.PlaytimeMin,
			IconURL:     t.IconURL,
		}
		if err := s.db.UpsertOwnedTitle(ot); err != nil {
			slog.Warn("stores: upsert owned", "err", err)
		}
	}
	_ = s.db.DeleteOwnedTitlesOlderThan(accountID, start) // drop no-longer-owned
	_ = s.mergeAccount(accountID)
	_ = s.db.SetAccountSyncStatus(accountID, time.Now().Unix(), "")
	s.emitEv("stores:account-done", map[string]any{"accountId": accountID, "count": len(titles)})
	return nil
}

// SyncAll syncs every enabled account sequentially.
func (s *Service) SyncAll(ctx context.Context) {
	accts, err := s.db.ListStoreAccounts()
	if err != nil {
		return
	}
	for _, a := range accts {
		if !a.Enabled {
			continue
		}
		_ = s.SyncAccount(ctx, a.ID)
	}
	s.emitEv("stores:sync-done", nil)
	s.emitEv("stores:library-changed", nil)
}

// GetAggregatedLibrary builds one merged card per game that is owned or
// installed: per-(store, account) ownership facts + the local installs.
func (s *Service) GetAggregatedLibrary() ([]*domain.LibraryCard, error) {
	games, err := s.db.ListGames()
	if err != nil {
		return nil, err
	}
	accts, _ := s.db.ListStoreAccounts()
	acctName := make(map[string]string, len(accts))
	for _, a := range accts {
		acctName[a.ID] = a.DisplayName
	}

	cards := []*domain.LibraryCard{}

	// 1) Real games: installed (and/or with ownership linked to them).
	for _, g := range games {
		insts, _ := s.db.ListInstallations(g.ID)
		owned, _ := s.db.ListOwnedTitlesForGame(g.ID)
		if len(insts) == 0 && len(owned) == 0 {
			continue // save-only / unrelated games don't belong in the store library
		}
		facts := make([]*domain.OwnershipFact, 0, len(owned))
		for _, ot := range owned {
			facts = append(facts, &domain.OwnershipFact{
				Store:       ot.Store,
				AccountID:   ot.AccountID,
				AccountName: acctName[ot.AccountID],
				StoreAppID:  ot.StoreAppID,
				Owned:       true,
				Installed:   installedForStore(insts, ot.Store, ot.StoreAppID),
				PlaytimeMin: ot.PlaytimeMin,
			})
		}
		imageURL := ""
		for _, ot := range owned {
			if ot.IconURL != "" {
				imageURL = ot.IconURL
				break
			}
		}
		cards = append(cards, &domain.LibraryCard{
			Game:          g,
			Ownership:     facts,
			Installations: insts,
			AnyInstalled:  len(insts) > 0,
			ImageURL:      imageURL,
		})
	}

	// 2) Synthetic cards for owned-but-not-installed titles (game_id NULL) — no
	// stub games row needed. Group by Steam AppID or normalized title so the
	// same game across stores/accounts collapses into ONE card.
	all, _ := s.db.ListAllOwnedTitles()
	type grp struct {
		key   string
		title string
		steam int64
		image string
		facts []*domain.OwnershipFact
	}
	order := []string{}
	groups := map[string]*grp{}
	for _, ot := range all {
		if ot.GameID != "" {
			continue // already shown via a real (installed) game card above
		}
		key := "name:" + match.NormalizedNameKey(ot.Title)
		if ot.SteamAppID != 0 {
			key = "steam:" + strconv.FormatInt(ot.SteamAppID, 10)
		}
		g := groups[key]
		if g == nil {
			g = &grp{key: key, title: ot.Title, steam: ot.SteamAppID}
			groups[key] = g
			order = append(order, key)
		}
		if ot.SteamAppID != 0 {
			g.steam = ot.SteamAppID
		}
		if g.image == "" && ot.IconURL != "" {
			g.image = ot.IconURL
		}
		g.facts = append(g.facts, &domain.OwnershipFact{
			Store:       ot.Store,
			AccountID:   ot.AccountID,
			AccountName: acctName[ot.AccountID],
			StoreAppID:  ot.StoreAppID,
			Owned:       true,
			Installed:   false,
			PlaytimeMin: ot.PlaytimeMin,
		})
	}
	for _, key := range order {
		g := groups[key]
		cards = append(cards, &domain.LibraryCard{
			Game:         &domain.Game{ID: "owned:" + g.key, Name: g.title, SteamAppID: g.steam},
			Ownership:    g.facts,
			AnyInstalled: false,
			ImageURL:     g.image,
		})
	}

	sort.Slice(cards, func(i, j int) bool {
		if cards[i].AnyInstalled != cards[j].AnyInstalled {
			return cards[i].AnyInstalled // installed first
		}
		return cards[i].Game.Name < cards[j].Game.Name
	})
	return cards, nil
}

func installedForStore(insts []*domain.Installation, store domain.SourceKind, appID string) bool {
	for _, i := range insts {
		if i.Source != store {
			continue
		}
		if appID == "" || i.SourceAppID == "" || i.SourceAppID == appID {
			return true
		}
	}
	return false
}
