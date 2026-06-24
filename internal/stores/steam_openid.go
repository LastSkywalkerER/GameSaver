package stores

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// steamOpenIDLogin runs "Sign in through Steam" (OpenID 2.0) over a loopback
// HTTP server: opens the Steam login page in the browser, captures the redirect
// to 127.0.0.1, verifies the assertion, and returns the SteamID64. No password
// or API key touches us — Steam authenticates in the browser and only hands
// back the public SteamID.
func steamOpenIDLogin(ctx context.Context, hc *http.Client, openBrowser func(string)) (string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port
	realm := fmt.Sprintf("http://127.0.0.1:%d", port)
	returnTo := realm + "/cb"

	auth := url.Values{
		"openid.ns":         {"http://specs.openid.net/auth/2.0"},
		"openid.mode":       {"checkid_setup"},
		"openid.return_to":  {returnTo},
		"openid.realm":      {realm},
		"openid.identity":   {"http://specs.openid.net/auth/2.0/identifier_select"},
		"openid.claimed_id": {"http://specs.openid.net/auth/2.0/identifier_select"},
	}
	authURL := "https://steamcommunity.com/openid/login?" + auth.Encode()

	resultCh := make(chan string, 1)
	errCh := make(chan error, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/cb", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		id := steamID64Re.FindString(q.Get("openid.claimed_id"))
		if id == "" || !verifySteamOpenID(ctx, hc, q) {
			fmt.Fprint(w, openIDPage("Не удалось подтвердить вход Steam. Можно закрыть вкладку."))
			select {
			case errCh <- errors.New("Steam OpenID: проверка не пройдена"):
			default:
			}
			return
		}
		fmt.Fprint(w, openIDPage("Готово! Steam подключён. Закрой вкладку и вернись в GameSaver."))
		select {
		case resultCh <- id:
		default:
		}
	})
	srv := &http.Server{Handler: mux}
	go srv.Serve(ln)
	defer srv.Close()

	openBrowser(authURL)

	select {
	case id := <-resultCh:
		return id, nil
	case err := <-errCh:
		return "", err
	case <-time.After(3 * time.Minute):
		return "", errors.New("истекло время ожидания входа Steam (3 мин)")
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// verifySteamOpenID confirms the assertion is genuinely from Steam (prevents a
// crafted callback from injecting someone else's SteamID).
func verifySteamOpenID(ctx context.Context, hc *http.Client, q url.Values) bool {
	v := url.Values{}
	for k, vals := range q {
		v[k] = vals
	}
	v.Set("openid.mode", "check_authentication")
	req, err := http.NewRequestWithContext(ctx, "POST", "https://steamcommunity.com/openid/login", strings.NewReader(v.Encode()))
	if err != nil {
		return false
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := hc.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return strings.Contains(string(b), "is_valid:true")
}

func openIDPage(msg string) string {
	return "<!doctype html><html><head><meta charset=utf-8><title>GameSaver</title></head>" +
		"<body style='font-family:system-ui;background:#0f1015;color:#eee;display:grid;place-items:center;height:100vh;margin:0'>" +
		"<h2 style='font-weight:600'>" + msg + "</h2></body></html>"
}
