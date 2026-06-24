package stores

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"GameSaver/internal/domain"
	"GameSaver/internal/secrets"
)

// steamProvider reads owned games either from the PUBLIC community profile
// (keyless XML — works after "Sign in through Steam") or, as a fallback, the
// Web API with a user-supplied key. SteamID64 is public, not a secret.
type steamProvider struct{ hc *http.Client }

func (p *steamProvider) Store() domain.SourceKind   { return domain.SourceSteam }
func (p *steamProvider) LoginURL() (string, string) { return "", "" } // browser login is its own method (OpenID)

var steamID64Re = regexp.MustCompile(`7656\d{13}`)

func (p *steamProvider) AddAccount(ctx context.Context, in AddAccountInput, v *secrets.Vault) (AccountInfo, error) {
	key := strings.TrimSpace(in.SteamKey)
	if key == "" {
		if tb, err := loadTokens(v, domain.SourceSteam, "*"); err == nil {
			key = tb.APIKey
		}
	}
	if key != "" {
		_ = saveTokens(v, domain.SourceSteam, "*", tokenBundle{APIKey: key})
	}

	steamID, err := p.resolveSteamID(ctx, key, strings.TrimSpace(in.SteamRef))
	if err != nil {
		return AccountInfo{}, err
	}
	// Persona: prefer the API (if a key is set), else the keyless profile XML.
	name, avatar := "", ""
	if key != "" {
		name, avatar = p.persona(ctx, key, steamID)
	}
	if name == "" {
		name, avatar = p.personaXML(ctx, steamID)
	}
	if name == "" {
		name = steamID
	}
	return AccountInfo{ExternalID: steamID, DisplayName: name, AvatarURL: avatar}, nil
}

func (p *steamProvider) resolveSteamID(ctx context.Context, key, ref string) (string, error) {
	if ref == "" {
		return "", errors.New("укажи SteamID64 или войди через Steam")
	}
	if m := steamID64Re.FindString(ref); m != "" {
		return m, nil
	}
	// Vanity name needs the Web API to resolve.
	if key == "" {
		return "", errors.New("для vanity-имени нужен API key — или используй «Вход через Steam» / SteamID64")
	}
	vanity := strings.Trim(ref, "/")
	if i := strings.LastIndex(vanity, "/"); i >= 0 {
		vanity = vanity[i+1:]
	}
	u := "https://api.steampowered.com/ISteamUser/ResolveVanityURL/v1/?key=" +
		url.QueryEscape(key) + "&vanityurl=" + url.QueryEscape(vanity)
	var out struct {
		Response struct {
			SteamID string `json:"steamid"`
			Success int    `json:"success"`
		} `json:"response"`
	}
	req, _ := http.NewRequest("GET", u, nil)
	if err := httpJSON(ctx, p.hc, req, &out); err != nil {
		return "", err
	}
	if out.Response.Success != 1 || out.Response.SteamID == "" {
		return "", fmt.Errorf("не удалось распознать аккаунт «%s»", ref)
	}
	return out.Response.SteamID, nil
}

func (p *steamProvider) persona(ctx context.Context, key, steamID string) (string, string) {
	u := "https://api.steampowered.com/ISteamUser/GetPlayerSummaries/v2/?key=" +
		url.QueryEscape(key) + "&steamids=" + steamID
	var out struct {
		Response struct {
			Players []struct {
				PersonaName string `json:"personaname"`
				Avatar      string `json:"avatarfull"`
			} `json:"players"`
		} `json:"response"`
	}
	req, _ := http.NewRequest("GET", u, nil)
	if err := httpJSON(ctx, p.hc, req, &out); err != nil || len(out.Response.Players) == 0 {
		return "", ""
	}
	return out.Response.Players[0].PersonaName, out.Response.Players[0].Avatar
}

// steamBrowserUA — steamcommunity endpoints are flaky for the default Go UA;
// present as a browser.
const steamBrowserUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0 Safari/537.36"

// communityGet fetches a steamcommunity.com URL with browser-like headers.
func (p *steamProvider) communityGet(ctx context.Context, u string) ([]byte, int, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", u, nil)
	req.Header.Set("User-Agent", steamBrowserUA)
	req.Header.Set("Accept", "text/xml,application/xml,text/html;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	resp, err := p.hc.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	return body, resp.StatusCode, nil
}

// personaXML reads the display name + avatar from the public profile XML (no key).
func (p *steamProvider) personaXML(ctx context.Context, steamID string) (string, string) {
	body, status, err := p.communityGet(ctx, "https://steamcommunity.com/profiles/"+steamID+"?xml=1")
	if err != nil || status/100 != 2 {
		return "", ""
	}
	var prof struct {
		SteamID    string `xml:"steamID"`
		AvatarFull string `xml:"avatarFull"`
	}
	if err := xml.Unmarshal(body, &prof); err != nil {
		return "", ""
	}
	return strings.TrimSpace(prof.SteamID), strings.TrimSpace(prof.AvatarFull)
}

func (p *steamProvider) FetchOwned(ctx context.Context, acct *domain.StoreAccount, v *secrets.Vault) ([]OwnedTitle, error) {
	// 1) Keyless public-profile games page (works after "Sign in through Steam").
	titles, xmlErr := p.fetchOwnedKeyless(ctx, acct.ExternalID)
	if xmlErr == nil && len(titles) > 0 {
		return titles, nil
	}
	// 2) Web API key fallback — more reliable than the community endpoint when
	// it's flaky (still respects the same public/private game-details setting).
	if tb, err := loadTokens(v, domain.SourceSteam, "*"); err == nil && tb.APIKey != "" {
		if t2, err2 := p.fetchOwnedAPI(ctx, tb.APIKey, acct.ExternalID); err2 == nil && len(t2) > 0 {
			return t2, nil
		}
	}
	if xmlErr != nil {
		return nil, fmt.Errorf("%v — если профиль точно публичный, нажми ↻ ещё раз или добавь Web API key (Steam → «ключ»)", xmlErr)
	}
	return nil, errors.New("библиотека пуста или «Игровые данные» приватны — в Steam: Профиль → Приватность → «Игровые данные» = Открыто (применяется не сразу; затем нажми ↻)")
}

// fetchOwnedKeyless reads owned games from the PUBLIC community games page by
// parsing its embedded `var rgGames = [...]` JSON. Steam's old ?xml=1 endpoint
// now serves the HTML page instead of XML (confirmed from logs), so we parse the
// page directly.
func (p *steamProvider) fetchOwnedKeyless(ctx context.Context, steamID string) ([]OwnedTitle, error) {
	u := "https://steamcommunity.com/profiles/" + steamID + "/games/?tab=all&l=english"
	body, status, err := p.communityGet(ctx, u)
	if err != nil {
		return nil, err
	}
	if status/100 != 2 {
		return nil, fmt.Errorf("steam community http %d", status)
	}
	html := string(body)
	arr := extractRgGames(html)
	if arr == "" {
		if strings.Contains(html, "profile_private_info") ||
			strings.Contains(html, "games on this account are hidden") ||
			strings.Contains(html, "This profile is private") {
			return nil, errors.New("в Steam «Игровые данные» приватны — Профиль → Приватность → «Игровые данные» = Открыто")
		}
		slog.Warn("steam: rgGames not found on games page", "steamID", steamID, "bytes", len(html))
		return nil, errors.New("не нашёл список игр на странице Steam — добавь Web API key")
	}
	var raw []struct {
		AppID        int64  `json:"appid"`
		Name         string `json:"name"`
		HoursForever string `json:"hours_forever"`
	}
	if err := json.Unmarshal([]byte(arr), &raw); err != nil {
		slog.Warn("steam: rgGames parse failed", "steamID", steamID, "err", err)
		return nil, fmt.Errorf("Steam: не удалось разобрать список игр")
	}
	slog.Info("steam: owned via games page", "steamID", steamID, "count", len(raw))
	titles := make([]OwnedTitle, 0, len(raw))
	for _, g := range raw {
		var mins int64
		if g.HoursForever != "" {
			if h, err := strconv.ParseFloat(strings.ReplaceAll(g.HoursForever, ",", ""), 64); err == nil {
				mins = int64(h * 60)
			}
		}
		titles = append(titles, OwnedTitle{
			StoreAppID:  strconv.FormatInt(g.AppID, 10),
			Title:       g.Name,
			SteamAppID:  g.AppID,
			PlaytimeMin: mins,
		})
	}
	return titles, nil
}

// extractRgGames pulls the `var rgGames = [ ... ];` JSON array out of the games
// page HTML via string-aware bracket matching (a game name may contain ']').
func extractRgGames(html string) string {
	idx := strings.Index(html, "rgGames")
	if idx < 0 {
		return ""
	}
	start := strings.IndexByte(html[idx:], '[')
	if start < 0 {
		return ""
	}
	start += idx
	depth, inStr, esc := 0, false, false
	for i := start; i < len(html); i++ {
		c := html[i]
		if inStr {
			switch {
			case esc:
				esc = false
			case c == '\\':
				esc = true
			case c == '"':
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return html[start : i+1]
			}
		}
	}
	return ""
}

func (p *steamProvider) fetchOwnedAPI(ctx context.Context, key, steamID string) ([]OwnedTitle, error) {
	u := "https://api.steampowered.com/IPlayerService/GetOwnedGames/v1/?key=" + url.QueryEscape(key) +
		"&steamid=" + steamID + "&include_appinfo=1&include_played_free_games=1&format=json"
	var out struct {
		Response struct {
			Games []struct {
				AppID           int64  `json:"appid"`
				Name            string `json:"name"`
				PlaytimeForever int64  `json:"playtime_forever"`
				ImgIconURL      string `json:"img_icon_url"`
			} `json:"games"`
		} `json:"response"`
	}
	req, _ := http.NewRequest("GET", u, nil)
	if err := httpJSON(ctx, p.hc, req, &out); err != nil {
		return nil, err
	}
	titles := make([]OwnedTitle, 0, len(out.Response.Games))
	for _, g := range out.Response.Games {
		icon := ""
		if g.ImgIconURL != "" {
			icon = fmt.Sprintf("https://media.steampowered.com/steamcommunity/public/images/apps/%d/%s.jpg", g.AppID, g.ImgIconURL)
		}
		titles = append(titles, OwnedTitle{
			StoreAppID:  strconv.FormatInt(g.AppID, 10),
			Title:       g.Name,
			SteamAppID:  g.AppID,
			PlaytimeMin: g.PlaytimeForever,
			IconURL:     icon,
		})
	}
	return titles, nil
}
