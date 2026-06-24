// Package stores connects online store libraries (Steam, GOG, Epic, EA) with
// multiple accounts each, fetches each account's OWNED titles, and merges them
// with locally-installed games into one aggregated library (#5).
//
// 🔴 Tokens/keys live ONLY in the DPAPI secrets vault, never in settings.json,
// logs, or the repo. The GOG/Epic client_id/secret constants are the PUBLIC
// launcher credentials (embedded by every open-source client like gogdl /
// Legendary) — they identify the launcher, not the user.
package stores

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"

	"GameSaver/internal/domain"
	"GameSaver/internal/secrets"
)

// OwnedTitle is a store-agnostic owned game returned by a Provider.
type OwnedTitle struct {
	StoreAppID  string // store-specific id (steam appid / gog product id / epic catalogItemId / ea offer)
	Title       string
	SteamAppID  int64 // populated for Steam — the strong cross-store merge key
	PlaytimeMin int64
	IconURL     string
}

// AccountInfo identifies a connected account after login.
type AccountInfo struct {
	ExternalID  string
	DisplayName string
	AvatarURL   string
}

// AddAccountInput carries whatever a store needs to add an account.
type AddAccountInput struct {
	SteamKey string // Steam Web API key (shared across Steam accounts)
	SteamRef string // SteamID64 / vanity / profile URL (Steam); display label (EA)
	AuthCode string // OAuth code or pasted redirect URL (GOG/Epic)
}

// Provider is one store integration.
type Provider interface {
	Store() domain.SourceKind
	// LoginURL returns the URL to open for an interactive login plus a hint of
	// what to paste back, or ("","") for a non-interactive store (Steam/EA).
	LoginURL() (url, hint string)
	// AddAccount validates input, performs any token exchange, persists tokens
	// to the vault, and returns the account identity.
	AddAccount(ctx context.Context, in AddAccountInput, v *secrets.Vault) (AccountInfo, error)
	// FetchOwned returns the account's owned titles, refreshing tokens via the
	// vault as needed.
	FetchOwned(ctx context.Context, acct *domain.StoreAccount, v *secrets.Vault) ([]OwnedTitle, error)
}

// tokenBundle is the JSON we DPAPI-encrypt per (store, account). Steam stores
// only APIKey under account "*"; OAuth stores keep access/refresh/expiry.
type tokenBundle struct {
	APIKey       string `json:"apiKey,omitempty"`
	AccessToken  string `json:"accessToken,omitempty"`
	RefreshToken string `json:"refreshToken,omitempty"`
	ExpiresAt    int64  `json:"expiresAt,omitempty"`
}

func saveTokens(v *secrets.Vault, store domain.SourceKind, account string, tb tokenBundle) error {
	b, err := json.Marshal(tb)
	if err != nil {
		return err
	}
	return v.Set(string(store), account, b)
}

func loadTokens(v *secrets.Vault, store domain.SourceKind, account string) (tokenBundle, error) {
	b, err := v.Get(string(store), account)
	if err != nil {
		return tokenBundle{}, err
	}
	var tb tokenBundle
	_ = json.Unmarshal(b, &tb)
	return tb, nil
}

// httpJSON runs req and decodes a JSON body into out. Non-2xx is an error with
// a short body snippet for diagnostics (never logged with secrets).
func httpJSON(ctx context.Context, hc *http.Client, req *http.Request, out any) error {
	resp, err := hc.Do(req.WithContext(ctx))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 400))
		return fmt.Errorf("http %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// httpRaw runs req and returns the raw body + status (for endpoints we want to
// log on failure, e.g. the experimental EA flow).
func httpRaw(ctx context.Context, hc *http.Client, req *http.Request) ([]byte, int, error) {
	resp, err := hc.Do(req.WithContext(ctx))
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	return body, resp.StatusCode, nil
}

// snippet trims a body to n chars for safe logging.
func snippet(b []byte, n int) string {
	s := strings.TrimSpace(string(b))
	if len(s) > n {
		return s[:n]
	}
	return s
}

// extractCode pulls an OAuth code from a pasted code or a redirect URL.
func extractCode(input string, re *regexp.Regexp) string {
	input = strings.TrimSpace(input)
	if m := re.FindStringSubmatch(input); len(m) > 1 {
		return m[1]
	}
	// A bare code with no spaces / URL punctuation: use it as-is.
	if input != "" && !strings.ContainsAny(input, " \t\n/?&=\"") {
		return input
	}
	return ""
}
