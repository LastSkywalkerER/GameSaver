package stores

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"GameSaver/internal/domain"
	"GameSaver/internal/secrets"
)

// Public Epic launcher OAuth credentials (used by Legendary / Heroic). EXPERIMENTAL:
// unofficial endpoints that have changed historically.
const (
	epicClientID     = "34a02cf8f4414e29b15921876da36f9a"
	epicClientSecret = "daafbccc737745039dffe53d94fc76cf"
	epicTokenURL     = "https://account-public-service-prod.ol.epicgames.com/account/api/oauth/token"
	epicLibraryURL   = "https://library-service.live.use1a.on.epicgames.com/library/api/public/items"
	epicCatalogHost  = "https://catalog-public-service-prod06.ol.epicgames.com"
)

type epicProvider struct{ hc *http.Client }

func (p *epicProvider) Store() domain.SourceKind { return domain.SourceEpic }

func (p *epicProvider) LoginURL() (string, string) {
	// Log in FIRST, then Epic redirects to api/redirect which prints the real
	// authorizationCode. Hitting api/redirect directly while logged out returns
	// authorizationCode:null (the reported issue).
	redirect := url.QueryEscape("https://www.epicgames.com/id/api/redirect?clientId=" + epicClientID + "&responseType=code")
	return "https://www.epicgames.com/id/login?redirectUrl=" + redirect,
		"Войди в Epic. После входа откроется страница с JSON — скопируй значение authorizationCode (длинная hex-строка) и вставь сюда."
}

var epicCodeRe = regexp.MustCompile(`authorizationCode["\s:=]+([0-9a-fA-F]{20,})`)

func (p *epicProvider) AddAccount(ctx context.Context, in AddAccountInput, v *secrets.Vault) (AccountInfo, error) {
	code := extractCode(in.AuthCode, epicCodeRe)
	if code == "" {
		return AccountInfo{}, errors.New("вставь authorizationCode из Epic")
	}
	tb, accountID, name, err := p.token(ctx, url.Values{
		"grant_type": {"authorization_code"},
		"code":       {code},
		"token_type": {"eg1"},
	})
	if err != nil {
		return AccountInfo{}, err
	}
	if err := saveTokens(v, domain.SourceEpic, accountID, tb); err != nil {
		return AccountInfo{}, err
	}
	if name == "" {
		name = "Epic " + accountID
	}
	return AccountInfo{ExternalID: accountID, DisplayName: name}, nil
}

func (p *epicProvider) token(ctx context.Context, form url.Values) (tokenBundle, string, string, error) {
	req, _ := http.NewRequest("POST", epicTokenURL, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(epicClientID+":"+epicClientSecret)))
	var out struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresAt    string `json:"expires_at"`
		AccountID    string `json:"account_id"`
		DisplayName  string `json:"displayName"`
	}
	if err := httpJSON(ctx, p.hc, req, &out); err != nil {
		return tokenBundle{}, "", "", err
	}
	if out.AccessToken == "" {
		return tokenBundle{}, "", "", errors.New("Epic: пустой ответ токена")
	}
	exp := time.Now().Unix() + 7*3600
	if t, err := time.Parse(time.RFC3339, out.ExpiresAt); err == nil {
		exp = t.Unix() - 60
	}
	return tokenBundle{AccessToken: out.AccessToken, RefreshToken: out.RefreshToken, ExpiresAt: exp}, out.AccountID, out.DisplayName, nil
}

func (p *epicProvider) ensureToken(ctx context.Context, v *secrets.Vault, acct *domain.StoreAccount) (string, error) {
	tb, err := loadTokens(v, domain.SourceEpic, acct.ExternalID)
	if err != nil {
		return "", errors.New("Epic: нет токена — переподключи аккаунт")
	}
	if tb.AccessToken != "" && tb.ExpiresAt > time.Now().Unix() {
		return tb.AccessToken, nil
	}
	ntb, _, _, err := p.token(ctx, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {tb.RefreshToken},
		"token_type":    {"eg1"},
	})
	if err != nil {
		return "", err
	}
	_ = saveTokens(v, domain.SourceEpic, acct.ExternalID, ntb)
	return ntb.AccessToken, nil
}

type epicRecord struct {
	Namespace     string `json:"namespace"`
	CatalogItemID string `json:"catalogItemId"`
	AppName       string `json:"appName"`
}

func (p *epicProvider) FetchOwned(ctx context.Context, acct *domain.StoreAccount, v *secrets.Vault) ([]OwnedTitle, error) {
	token, err := p.ensureToken(ctx, v, acct)
	if err != nil {
		return nil, err
	}
	// 1) Page through library items (ids only).
	var records []epicRecord
	cursor := ""
	for i := 0; i < 100; i++ {
		u := epicLibraryURL + "?includeMetadata=true"
		if cursor != "" {
			u += "&cursor=" + url.QueryEscape(cursor)
		}
		req, _ := http.NewRequest("GET", u, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		var out struct {
			Records          []epicRecord `json:"records"`
			ResponseMetadata struct {
				NextCursor string `json:"nextCursor"`
			} `json:"responseMetadata"`
		}
		if err := httpJSON(ctx, p.hc, req, &out); err != nil {
			return nil, err
		}
		records = append(records, out.Records...)
		if out.ResponseMetadata.NextCursor == "" || len(records) > 5000 {
			break
		}
		cursor = out.ResponseMetadata.NextCursor
	}
	// 2) Drop Unreal Engine Marketplace assets early (namespace "ue") — plugins,
	// 3D assets, dev kits; not games, and they flood the library.
	var gameRecs []epicRecord
	for _, r := range records {
		if r.Namespace == "ue" {
			continue
		}
		gameRecs = append(gameRecs, r)
	}
	// 3) Classify + title via the per-namespace catalog; keep ONLY catalog-
	// confirmed games with a real title (filters assets / apps / DLC and delisted
	// junk — no appName fallback, which produced the "wrong list" earlier).
	catalog := p.catalogItems(ctx, token, gameRecs)
	titles := make([]OwnedTitle, 0, len(gameRecs))
	for _, r := range gameRecs {
		ci, ok := catalog[r.CatalogItemID]
		if !ok || !ci.isGame || ci.title == "" {
			continue
		}
		titles = append(titles, OwnedTitle{StoreAppID: r.CatalogItemID, Title: ci.title, IconURL: ci.image})
	}
	slog.Info("epic: owned games", "account", acct.ExternalID,
		"raw", len(records), "afterUE", len(gameRecs), "resolved", len(catalog), "games", len(titles))
	return titles, nil
}

type epicCatalogItem struct {
	title  string
	isGame bool
	image  string
}

// catalogItems resolves each item's title + whether it's a game via Epic's
// PER-NAMESPACE catalog endpoint. The cross-namespace bulk form (?id=ns:id)
// 404s the WHOLE batch if any single id is delisted (confirmed from logs), so we
// fan out one request per namespace — each Epic product has its own namespace —
// with bounded concurrency.
func (p *epicProvider) catalogItems(ctx context.Context, token string, recs []epicRecord) map[string]epicCatalogItem {
	byNS := map[string][]string{}
	for _, r := range recs {
		if r.Namespace == "" || r.CatalogItemID == "" {
			continue
		}
		byNS[r.Namespace] = append(byNS[r.Namespace], r.CatalogItemID)
	}

	out := map[string]epicCatalogItem{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8) // don't hammer Epic
	for ns, ids := range byNS {
		wg.Add(1)
		sem <- struct{}{}
		go func(ns string, ids []string) {
			defer wg.Done()
			defer func() { <-sem }()
			q := url.Values{}
			q.Set("country", "US")
			q.Set("locale", "en-US")
			q.Set("includeMainGameDetails", "true")
			for _, id := range ids {
				q.Add("id", id)
			}
			u := fmt.Sprintf("%s/catalog/api/shared/namespace/%s/bulk/items?%s", epicCatalogHost, url.PathEscape(ns), q.Encode())
			req, _ := http.NewRequest("GET", u, nil)
			req.Header.Set("Authorization", "Bearer "+token)
			var m map[string]struct {
				Title      string `json:"title"`
				Categories []struct {
					Path string `json:"path"`
				} `json:"categories"`
				KeyImages []struct {
					Type string `json:"type"`
					URL  string `json:"url"`
				} `json:"keyImages"`
			}
			if err := httpJSON(ctx, p.hc, req, &m); err != nil {
				return // delisted / not in catalog — skip this namespace
			}
			mu.Lock()
			for id, item := range m {
				isGame := false
				for _, c := range item.Categories {
					seg := c.Path
					if k := strings.IndexByte(seg, '/'); k >= 0 {
						seg = seg[:k]
					}
					if seg == "games" {
						isGame = true
						break
					}
				}
				// Prefer a tall/portrait cover for the card.
				img := ""
				for _, want := range []string{"OfferImageTall", "DieselGameBoxTall", "Thumbnail", "DieselStoreFrontTall"} {
					for _, ki := range item.KeyImages {
						if ki.Type == want && ki.URL != "" {
							img = ki.URL
							break
						}
					}
					if img != "" {
						break
					}
				}
				out[id] = epicCatalogItem{title: item.Title, isGame: isGame, image: img}
			}
			mu.Unlock()
		}(ns, ids)
	}
	wg.Wait()
	return out
}
