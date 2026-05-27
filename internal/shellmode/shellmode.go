// Package shellmode wraps everything needed to register GameSaver as the
// Windows user shell (HKCU\...\Winlogon\Shell). The actual binary that
// owns the slot is gamesaver-watchdog.exe — a tiny supervisor that
// relaunches GameSaver on crash and listens for a global escape hotkey.
// We download the watchdog from the GitHub release on demand so the
// main GameSaver.exe stays small for users who never enable this.
package shellmode

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"golang.org/x/sys/windows/registry"
)

const (
	winlogonKey   = `Software\Microsoft\Windows NT\CurrentVersion\Winlogon`
	shellValue    = `Shell`
	releasesAPI   = "https://api.github.com/repos/LastSkywalkerER/GameSaver/releases/latest"
	watchdogAsset = "gamesaver-watchdog-windows-amd64.exe"
	checksumAsset = "checksums.txt"
)

// Paths bundles the on-disk locations used by the watchdog. Lives in
// %LOCALAPPDATA%\GameSaver\bin\ — same drive as the user data, not next
// to GameSaver.exe (so it survives reinstalls / portable moves).
type Paths struct {
	BinDir       string // %LOCALAPPDATA%\GameSaver\bin
	Watchdog     string // bin\gamesaver-watchdog.exe
	Target       string // bin\target.txt — path to GameSaver.exe written on enable
}

func ResolvePaths() (Paths, error) {
	la := os.Getenv("LOCALAPPDATA")
	if la == "" {
		return Paths{}, errors.New("LOCALAPPDATA env not set")
	}
	bin := filepath.Join(la, "GameSaver", "bin")
	return Paths{
		BinDir:   bin,
		Watchdog: filepath.Join(bin, "gamesaver-watchdog.exe"),
		Target:   filepath.Join(bin, "target.txt"),
	}, nil
}

// WatchdogPresent returns true if the watchdog binary is already downloaded.
func (p Paths) WatchdogPresent() bool {
	st, err := os.Stat(p.Watchdog)
	return err == nil && !st.IsDir() && st.Size() > 0
}

// IsRegistered returns true if HKCU\...\Winlogon\Shell points at our
// watchdog (i.e. the next logon will run us instead of Explorer).
func IsRegistered() (bool, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, winlogonKey, registry.QUERY_VALUE)
	if err != nil {
		// Key always exists on Windows — if open fails the user has bigger
		// problems than this feature.
		if errors.Is(err, registry.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	defer k.Close()
	val, _, err := k.GetStringValue(shellValue)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	paths, err := ResolvePaths()
	if err != nil {
		return false, err
	}
	return strings.EqualFold(strings.TrimSpace(val), paths.Watchdog), nil
}

// Enable wires shell mode: writes the watchdog path into the registry
// and remembers where GameSaver.exe lives so the watchdog can find it.
// Caller must ensure WatchdogPresent() is true first (call EnsureWatchdog).
func Enable(gameSaverExe string) error {
	paths, err := ResolvePaths()
	if err != nil {
		return err
	}
	if !paths.WatchdogPresent() {
		return errors.New("watchdog binary not downloaded yet")
	}
	if err := os.WriteFile(paths.Target, []byte(gameSaverExe), 0o644); err != nil {
		return fmt.Errorf("write target.txt: %w", err)
	}
	k, _, err := registry.CreateKey(registry.CURRENT_USER, winlogonKey, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("open Winlogon key: %w", err)
	}
	defer k.Close()
	if err := k.SetStringValue(shellValue, paths.Watchdog); err != nil {
		return fmt.Errorf("write Shell value: %w", err)
	}
	return nil
}

// Disable removes the Shell registration. We deliberately do NOT touch
// the watchdog binary or target.txt — leaving them around makes re-enable
// cheap and gives the user something to inspect if things went wrong.
func Disable() error {
	k, err := registry.OpenKey(registry.CURRENT_USER, winlogonKey, registry.SET_VALUE)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return nil
		}
		return err
	}
	defer k.Close()
	if err := k.DeleteValue(shellValue); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return err
	}
	return nil
}

// EnsureWatchdog downloads the watchdog from the latest GitHub release if
// it isn't already on disk, verifying the SHA-256 against checksums.txt
// from the same release. Returns the path the watchdog now lives at.
//
// We never auto-update an existing watchdog — once shell mode is enabled
// and working, an unexpected silent swap would risk breaking the user's
// logon. Re-download is explicit (delete the file or call ForceRedownload).
func EnsureWatchdog(ctx context.Context) (string, error) {
	paths, err := ResolvePaths()
	if err != nil {
		return "", err
	}
	if paths.WatchdogPresent() {
		return paths.Watchdog, nil
	}
	if err := os.MkdirAll(paths.BinDir, 0o755); err != nil {
		return "", fmt.Errorf("mkdir bin: %w", err)
	}
	if err := downloadWatchdog(ctx, paths); err != nil {
		return "", err
	}
	return paths.Watchdog, nil
}

func downloadWatchdog(ctx context.Context, paths Paths) error {
	if runtime.GOOS != "windows" {
		return errors.New("shell mode is Windows-only")
	}

	// 1. Fetch the latest release JSON to find asset download URLs.
	rel, err := fetchLatestRelease(ctx)
	if err != nil {
		return fmt.Errorf("query release: %w", err)
	}
	var watchdogURL, checksumURL string
	for _, a := range rel.Assets {
		switch a.Name {
		case watchdogAsset:
			watchdogURL = a.URL
		case checksumAsset:
			checksumURL = a.URL
		}
	}
	if watchdogURL == "" {
		return fmt.Errorf("release %s has no %s asset", rel.Tag, watchdogAsset)
	}

	// 2. Fetch the watchdog into a temp file alongside the destination so
	// the final rename is atomic on the same volume.
	tmp := paths.Watchdog + ".part"
	got, sum, err := fetchTo(ctx, watchdogURL, tmp)
	if err != nil {
		return fmt.Errorf("download watchdog: %w", err)
	}
	defer os.Remove(tmp) // no-op if rename succeeds

	// 3. Verify against checksums.txt if it's there. (If the release ever
	// ships without checksums we still install — the only realistic risk is
	// a HEAD-of-line man-in-the-middle, and we go via TLS to api.github.com.)
	if checksumURL != "" {
		expect, err := fetchExpectedSum(ctx, checksumURL)
		if err != nil {
			return fmt.Errorf("fetch checksums: %w", err)
		}
		if expect != "" && !strings.EqualFold(expect, sum) {
			return fmt.Errorf("checksum mismatch: got %s want %s (downloaded %d bytes)", sum, expect, got)
		}
	}

	if err := os.Rename(tmp, paths.Watchdog); err != nil {
		return fmt.Errorf("install watchdog: %w", err)
	}
	return nil
}

type releaseAsset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}
type release struct {
	Tag    string         `json:"tag_name"`
	Assets []releaseAsset `json:"assets"`
}

func fetchLatestRelease(ctx context.Context) (*release, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", releasesAPI, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "GameSaver-shellmode/1.0")
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("github %d", resp.StatusCode)
	}
	var r release
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, err
	}
	return &r, nil
}

func fetchTo(ctx context.Context, url, dst string) (int64, string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return 0, "", err
	}
	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return 0, "", fmt.Errorf("http %d", resp.StatusCode)
	}
	f, err := os.Create(dst)
	if err != nil {
		return 0, "", err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(f, io.TeeReader(resp.Body, h))
	if err != nil {
		return 0, "", err
	}
	return n, hex.EncodeToString(h.Sum(nil)), nil
}

func fetchExpectedSum(ctx context.Context, url string) (string, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return "", err
	}
	// checksums.txt has lines like "<sha256>  <filename>". Find ours.
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// "sha256  filename" or "sha256 *filename"
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		fn := strings.TrimPrefix(fields[1], "*")
		if fn == watchdogAsset {
			return fields[0], nil
		}
	}
	return "", nil
}
