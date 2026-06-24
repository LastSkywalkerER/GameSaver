package stores

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"GameSaver/internal/domain"
	"GameSaver/internal/secrets"
)

// Public GOG Galaxy client credentials (embedded by gogdl / Heroic). They
// identify the launcher, not the user; user tokens are DPAPI-vaulted.
const (
	gogClientID     = "46899977096215655"
	gogClientSecret = "9d85c43b1482497dbbce61f6e4aa173a433796eeae2ca8c5f6129f2dc4de46d9"
	gogRedirectURI  = "https://embed.gog.com/on_login_success?origin=client"
)

type gogProvider struct{ hc *http.Client }

func (p *gogProvider) Store() domain.SourceKind { return domain.SourceGOG }

func (p *gogProvider) LoginURL() (string, string) {
	u := "https://auth.gog.com/auth?client_id=" + gogClientID +
		"&redirect_uri=" + url.QueryEscape(gogRedirectURI) +
		"&response_type=code&layout=client2"
	return u, "Войди в GOG, затем скопируй адрес страницы (с «code=…») или сам код и вставь сюда."
}

var gogCodeRe = regexp.MustCompile(`code=([A-Za-z0-9_\-]+)`)

func (p *gogProvider) AddAccount(ctx context.Context, in AddAccountInput, v *secrets.Vault) (AccountInfo, error) {
	code := extractCode(in.AuthCode, gogCodeRe)
	if code == "" {
		return AccountInfo{}, errors.New("вставь код или URL с code=… из GOG")
	}
	tb, userID, err := p.exchange(ctx, url.Values{
		"grant_type":   {"authorization_code"},
		"code":         {code},
		"redirect_uri": {gogRedirectURI},
	})
	if err != nil {
		return AccountInfo{}, err
	}
	if err := saveTokens(v, domain.SourceGOG, userID, tb); err != nil {
		return AccountInfo{}, err
	}
	name, avatar := p.userData(ctx, tb.AccessToken)
	if name == "" {
		name = "GOG " + userID
	}
	return AccountInfo{ExternalID: userID, DisplayName: name, AvatarURL: avatar}, nil
}

// exchange hits auth.gog.com/token for both authorization_code and
// refresh_token grants, returning a fresh bundle + the GOG user id.
func (p *gogProvider) exchange(ctx context.Context, form url.Values) (tokenBundle, string, error) {
	form.Set("client_id", gogClientID)
	form.Set("client_secret", gogClientSecret)
	req, _ := http.NewRequest("GET", "https://auth.gog.com/token?"+form.Encode(), nil)
	var out struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
		UserID       string `json:"user_id"`
	}
	if err := httpJSON(ctx, p.hc, req, &out); err != nil {
		return tokenBundle{}, "", err
	}
	if out.AccessToken == "" {
		return tokenBundle{}, "", errors.New("GOG: пустой ответ токена")
	}
	tb := tokenBundle{
		AccessToken:  out.AccessToken,
		RefreshToken: out.RefreshToken,
		ExpiresAt:    time.Now().Unix() + out.ExpiresIn - 60,
	}
	return tb, out.UserID, nil
}

func (p *gogProvider) userData(ctx context.Context, token string) (string, string) {
	req, _ := http.NewRequest("GET", "https://embed.gog.com/userData.json", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	var out struct {
		Username string `json:"username"`
		Avatar   string `json:"avatar"`
	}
	if err := httpJSON(ctx, p.hc, req, &out); err != nil {
		return "", ""
	}
	return out.Username, out.Avatar
}

func (p *gogProvider) ensureToken(ctx context.Context, v *secrets.Vault, acct *domain.StoreAccount) (string, error) {
	tb, err := loadTokens(v, domain.SourceGOG, acct.ExternalID)
	if err != nil {
		return "", errors.New("GOG: нет токена — переподключи аккаунт")
	}
	if tb.AccessToken != "" && tb.ExpiresAt > time.Now().Unix() {
		return tb.AccessToken, nil
	}
	ntb, _, err := p.exchange(ctx, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {tb.RefreshToken},
	})
	if err != nil {
		return "", err
	}
	_ = saveTokens(v, domain.SourceGOG, acct.ExternalID, ntb)
	return ntb.AccessToken, nil
}

func (p *gogProvider) FetchOwned(ctx context.Context, acct *domain.StoreAccount, v *secrets.Vault) ([]OwnedTitle, error) {
	token, err := p.ensureToken(ctx, v, acct)
	if err != nil {
		return nil, err
	}
	titles := []OwnedTitle{}
	for page := 1; page <= 50; page++ {
		u := fmt.Sprintf("https://embed.gog.com/account/getFilteredProducts?mediaType=1&page=%d", page)
		req, _ := http.NewRequest("GET", u, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		var out struct {
			TotalPages int `json:"totalPages"`
			Products   []struct {
				ID    json.Number `json:"id"`
				Title string      `json:"title"`
				Image string      `json:"image"`
			} `json:"products"`
		}
		if err := httpJSON(ctx, p.hc, req, &out); err != nil {
			return nil, err
		}
		for _, pr := range out.Products {
			img := pr.Image
			if strings.HasPrefix(img, "//") {
				// GOG product image base → a real capsule render.
				img = "https:" + img + "_product_card_v2_mobile_slider_639.jpg"
			}
			titles = append(titles, OwnedTitle{StoreAppID: pr.ID.String(), Title: pr.Title, IconURL: img})
		}
		if out.TotalPages <= 0 || page >= out.TotalPages {
			break
		}
	}
	return titles, nil
}
