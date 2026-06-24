package stores

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"GameSaver/internal/domain"
	"GameSaver/internal/secrets"
)

// EA has NO official public library API. The desktop app reads owned games via
// the same unofficial endpoints we use here, so this is EXPERIMENTAL and
// fragile (EA changed these during the Origin→EA-app migration). Everything is
// logged to gamesaver.log so we can adjust when an endpoint moves.
//
// Flow: OAuth (response_type=token, redirect nucleus:rest → JSON with
// access_token, pasted by the user) → pid via gateway.ea.com → owned offers via
// the Origin entitlements endpoint → names via the per-offer Origin catalog.
const (
	eaClientID    = "EADOTCOM-WEB-SERVER"
	eaRedirectURI = "https://www.ea.com/login_successful"
	eaAuthURL     = "https://accounts.ea.com/connect/auth"
	eaPidURL      = "https://gateway.ea.com/proxy/identity/pids/me"
	eaEntURL      = "https://api1.origin.com/ecommerce2/entitlements/"
	eaOfferURL    = "https://api1.origin.com/ecommerce2/public/"
)

type eaProvider struct{ hc *http.Client }

func (p *eaProvider) Store() domain.SourceKind { return domain.SourceEA }

func (p *eaProvider) LoginURL() (string, string) {
	// EA's library gateway rejects web-login tokens ("auth token not allowed
	// for unknown client" — confirmed from logs), and the EA-app client the
	// gateway DOES trust uses a desktop-only (qrc://) redirect we can't capture
	// from a browser. So there is NO browser-paste path to the EA cloud library.
	// EA stays installed-only; installed EA games still appear via installations.
	// (The OAuth helpers below are kept dormant in case a usable client appears.)
	return "", "EA не отдаёт онлайн-библиотеку без своего клиента — показываются только установленные EA-игры."
}

// eaURLParam pulls a query/fragment param value out of a pasted redirect URL.
func eaURLParam(input, key string) string {
	i := strings.Index(input, key+"=")
	if i < 0 {
		return ""
	}
	rest := input[i+len(key)+1:]
	if j := strings.IndexAny(rest, "&\" \t\n"); j >= 0 {
		rest = rest[:j]
	}
	if dec, err := url.QueryUnescape(rest); err == nil && dec != "" {
		return dec
	}
	return rest
}

// exchangeCode turns an authorization code into an access token.
func (p *eaProvider) exchangeCode(ctx context.Context, code string) (string, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("client_id", eaClientID)
	form.Set("redirect_uri", eaRedirectURI)
	req, _ := http.NewRequest("POST", "https://accounts.ea.com/connect/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	body, status, err := httpRaw(ctx, p.hc, req)
	if err != nil {
		return "", err
	}
	if status/100 != 2 {
		slog.Warn("ea: token exchange failed", "status", status, "body", snippet(body, 300))
		return "", fmt.Errorf("EA: обмен кода не прошёл (код %d) — см. логи", status)
	}
	var t struct {
		AccessToken string `json:"access_token"`
	}
	_ = json.Unmarshal(body, &t)
	if t.AccessToken == "" {
		slog.Warn("ea: empty token from exchange", "body", snippet(body, 200))
		return "", errors.New("EA: пустой токен")
	}
	return t.AccessToken, nil
}

func (p *eaProvider) AddAccount(ctx context.Context, in AddAccountInput, v *secrets.Vault) (AccountInfo, error) {
	raw := strings.TrimSpace(in.AuthCode)
	token := eaURLParam(raw, "access_token")
	if token == "" {
		if code := eaURLParam(raw, "code"); code != "" {
			t, err := p.exchangeCode(ctx, code)
			if err != nil {
				return AccountInfo{}, err
			}
			token = t
		} else if raw != "" && !strings.ContainsAny(raw, " \t\n{}\"/") {
			token = raw // pasted a bare token
		}
	}
	if token == "" {
		return AccountInfo{}, errors.New("вставь URL после входа EA (с ?code=… или #access_token=…)")
	}
	pid, name, err := p.identity(ctx, token)
	if err != nil {
		return AccountInfo{}, err
	}
	_ = saveTokens(v, domain.SourceEA, pid, tokenBundle{AccessToken: token, ExpiresAt: time.Now().Unix() + 3000})
	if name == "" {
		name = "EA " + pid
	}
	return AccountInfo{ExternalID: pid, DisplayName: name}, nil
}

func (p *eaProvider) identity(ctx context.Context, token string) (pid, name string, err error) {
	req, _ := http.NewRequest("GET", eaPidURL, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	body, status, err := httpRaw(ctx, p.hc, req)
	if err != nil {
		return "", "", err
	}
	if status/100 != 2 {
		slog.Warn("ea: pids/me failed", "status", status, "body", snippet(body, 200))
		return "", "", fmt.Errorf("EA: вход не принят (код %d)", status)
	}
	var pr struct {
		Pid struct {
			PidID json.Number `json:"pidId"`
			Email string      `json:"email"`
		} `json:"pid"`
	}
	_ = json.Unmarshal(body, &pr)
	pid = pr.Pid.PidID.String()
	if pid == "" || pid == "0" {
		slog.Warn("ea: no pid in response", "body", snippet(body, 200))
		return "", "", errors.New("EA: не удалось получить ID аккаунта")
	}
	return pid, pr.Pid.Email, nil
}

func (p *eaProvider) FetchOwned(ctx context.Context, acct *domain.StoreAccount, v *secrets.Vault) ([]OwnedTitle, error) {
	tb, err := loadTokens(v, domain.SourceEA, acct.ExternalID)
	if err != nil || tb.AccessToken == "" {
		return nil, errors.New("EA: нет токена — переподключи аккаунт")
	}
	req, _ := http.NewRequest("GET", eaEntURL+acct.ExternalID+"?machine_hash=1", nil)
	req.Header.Set("Authorization", "Bearer "+tb.AccessToken)
	req.Header.Set("AuthToken", tb.AccessToken)
	req.Header.Set("X-Origin-Platform", "PCWIN")
	req.Header.Set("Accept", "application/json")
	body, status, err := httpRaw(ctx, p.hc, req)
	if err != nil {
		return nil, err
	}
	if status/100 != 2 {
		slog.Warn("ea: entitlements failed", "status", status, "body", snippet(body, 300))
		return nil, fmt.Errorf("EA: библиотека недоступна (код %d) — endpoint мог измениться", status)
	}
	var ent struct {
		Entitlements []struct {
			OfferID   string `json:"offerId"`
			OfferType string `json:"offerType"`
			Status    string `json:"status"`
		} `json:"entitlements"`
	}
	if err := json.Unmarshal(body, &ent); err != nil {
		slog.Warn("ea: entitlements parse failed", "err", err, "body", snippet(body, 300))
		return nil, errors.New("EA: не разобрал ответ библиотеки")
	}
	offers := []string{}
	for _, e := range ent.Entitlements {
		t := strings.ToLower(e.OfferType)
		if t == "basic-game" || strings.Contains(t, "game") {
			offers = append(offers, e.OfferID)
		}
	}
	slog.Info("ea: entitlements", "account", acct.ExternalID, "total", len(ent.Entitlements), "gameOffers", len(offers))

	names := p.offerNames(ctx, tb.AccessToken, offers)
	titles := make([]OwnedTitle, 0, len(offers))
	for _, off := range offers {
		name := names[off]
		if name == "" {
			continue // skip unresolved rather than show raw offerIds
		}
		titles = append(titles, OwnedTitle{StoreAppID: off, Title: name})
	}
	slog.Info("ea: owned games", "account", acct.ExternalID, "named", len(titles))
	if len(titles) == 0 && len(offers) > 0 {
		return nil, errors.New("EA: нашёл игры, но не получил названия — endpoint каталога мог измениться (см. логи)")
	}
	return titles, nil
}

// offerNames resolves each offerId to a display name via the per-offer Origin
// catalog, concurrently.
func (p *eaProvider) offerNames(ctx context.Context, token string, offers []string) map[string]string {
	out := map[string]string{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for _, off := range offers {
		wg.Add(1)
		sem <- struct{}{}
		go func(off string) {
			defer wg.Done()
			defer func() { <-sem }()
			req, _ := http.NewRequest("GET", eaOfferURL+url.PathEscape(off)+"?country=US&locale=en_US", nil)
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("Accept", "application/json")
			var d struct {
				I18n struct {
					DisplayName string `json:"displayName"`
				} `json:"i18n"`
				ItemName    string `json:"itemName"`
				DisplayName string `json:"displayName"`
			}
			if err := httpJSON(ctx, p.hc, req, &d); err != nil {
				return
			}
			name := d.I18n.DisplayName
			if name == "" {
				name = d.DisplayName
			}
			if name == "" {
				name = d.ItemName
			}
			if name != "" {
				mu.Lock()
				out[off] = name
				mu.Unlock()
			}
		}(off)
	}
	wg.Wait()
	return out
}
