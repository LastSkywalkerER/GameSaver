// Package updater implements a GitHub-Releases-backed self-updater for the
// portable .zip distribution shipped by our CI workflow.
//
// Flow:
//   1. Check() hits the GitHub Releases API and parses the latest tag + assets.
//   2. UI shows a banner if the latest semver > running version (and the user
//      hasn't pinned-skipped that version).
//   3. On user "Update", Apply() downloads the .zip asset, verifies its SHA-256
//      against checksums.txt from the same release, extracts the new exe in
//      memory and hands the bytes to minio/selfupdate which performs the
//      Windows-safe atomic swap (rename running exe → write new → restart on
//      next launch).
//
// Local dev builds (AppVersion="dev") short-circuit Check() so devs aren't
// nagged.
package updater

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/minio/selfupdate"
)

// UpdateInfo is what the UI receives when an update is available.
type UpdateInfo struct {
	Available    bool   `json:"available"`
	CurrentVer   string `json:"currentVersion"`
	LatestVer    string `json:"latestVersion"`
	ReleaseURL   string `json:"releaseUrl"`
	ReleaseNotes string `json:"releaseNotes,omitempty"`
	AssetURL     string `json:"assetUrl,omitempty"`
	AssetSize    int64  `json:"assetSize,omitempty"`
	SHA256       string `json:"sha256,omitempty"`
	PublishedAt  string `json:"publishedAt,omitempty"`
}

// Updater talks to a single GitHub repo's Releases.
type Updater struct {
	Owner          string
	Repo           string
	AssetSuffix    string // e.g. "-windows-amd64.zip"
	BinaryName     string // e.g. "GameSaver.exe"
	CurrentVersion string
	HTTPClient     *http.Client
}

// New builds an Updater wired to the GameSaver release stream.
func New(currentVersion string) *Updater {
	return &Updater{
		Owner:          "LastSkywalkerER",
		Repo:           "GameSaver",
		AssetSuffix:    "-windows-amd64.zip",
		BinaryName:     "GameSaver.exe",
		CurrentVersion: currentVersion,
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Check determines the latest release WITHOUT touching api.github.com.
//
// The GitHub REST API is rate-limited to 60 req/hour for unauthenticated
// clients and was returning 403 Forbidden, killing update detection. So we
// resolve the latest tag from the HTML redirect
// (github.com/<o>/<r>/releases/latest → …/releases/tag/<tag>) — plain web
// pages aren't subject to the API rate limit — and build the asset +
// checksum URLs from the well-known release-download pattern (these go to
// the objects CDN, also un-rate-limited). No API, no 403.
func (u *Updater) Check(ctx context.Context) (*UpdateInfo, error) {
	if u.CurrentVersion == "dev" || u.CurrentVersion == "" {
		return &UpdateInfo{Available: false, CurrentVer: u.CurrentVersion}, nil
	}
	latest, err := u.latestTagViaRedirect(ctx)
	if err != nil {
		return nil, err
	}
	info := &UpdateInfo{
		CurrentVer:  u.CurrentVersion,
		LatestVer:   latest,
		ReleaseURL:  fmt.Sprintf("https://github.com/%s/%s/releases/tag/%s", u.Owner, u.Repo, latest),
		PublishedAt: time.Now().Format(time.RFC3339),
	}
	if latest == "" || !versionGreater(latest, u.CurrentVersion) {
		return info, nil
	}
	// Predictable CDN URLs from our CI's asset naming — no API lookup.
	assetName := u.assetBaseName(latest) // GameSaver-<tag>-windows-amd64.zip
	info.AssetURL = fmt.Sprintf("https://github.com/%s/%s/releases/download/%s/%s",
		u.Owner, u.Repo, latest, assetName)
	checksumURL := fmt.Sprintf("https://github.com/%s/%s/releases/download/%s/checksums.txt",
		u.Owner, u.Repo, latest)
	info.SHA256 = u.fetchSha(ctx, checksumURL, assetName)
	info.ReleaseNotes = fmt.Sprintf("Подробности: %s", info.ReleaseURL)
	info.Available = true
	return info, nil
}

// latestTagViaRedirect reads the Location header of the (un-rate-limited)
// /releases/latest HTML endpoint, e.g. ".../releases/tag/v0.8.6" → "v0.8.6".
func (u *Updater) latestTagViaRedirect(ctx context.Context) (string, error) {
	url := fmt.Sprintf("https://github.com/%s/%s/releases/latest", u.Owner, u.Repo)
	// Don't follow the redirect — we just want the Location.
	client := &http.Client{
		Timeout:       u.HTTPClient.Timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	req.Header.Set("User-Agent", "GameSaver-updater")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	loc := resp.Header.Get("Location")
	if loc == "" {
		loc = resp.Request.URL.String() // no redirect (e.g. 200) — use final URL
	}
	i := strings.LastIndex(loc, "/tag/")
	if i < 0 {
		return "", fmt.Errorf("releases/latest: cannot parse tag from %q (status %s)", loc, resp.Status)
	}
	return strings.Trim(loc[i+len("/tag/"):], "/"), nil
}

// Apply downloads info.AssetURL, verifies SHA-256, extracts the binary from
// the zip and swaps it for the running exe. The caller should restart the
// process after this returns.
func (u *Updater) Apply(ctx context.Context, info *UpdateInfo) error {
	if info.AssetURL == "" {
		return fmt.Errorf("no asset url")
	}
	req, _ := http.NewRequestWithContext(ctx, "GET", info.AssetURL, nil)
	req.Header.Set("User-Agent", "GameSaver-updater")
	resp, err := u.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("download: %s", resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	// Verify SHA-256 if we have one.
	if info.SHA256 != "" {
		sum := sha256.Sum256(body)
		got := hex.EncodeToString(sum[:])
		if !strings.EqualFold(got, info.SHA256) {
			return fmt.Errorf("checksum mismatch: want %s got %s", info.SHA256, got)
		}
	}
	// Extract the exe from the zip.
	exe, err := extractBinary(body, u.BinaryName)
	if err != nil {
		return err
	}
	// minio/selfupdate handles the Windows-safe rename + write trick.
	if err := selfupdate.Apply(bytes.NewReader(exe), selfupdate.Options{}); err != nil {
		_ = selfupdate.RollbackError(err)
		return err
	}
	return nil
}

func (u *Updater) fetchSha(ctx context.Context, url, want string) string {
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	req.Header.Set("User-Agent", "GameSaver-updater")
	resp, err := u.HTTPClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return ""
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return ""
	}
	wantLow := strings.ToLower(want)
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		// "<sha>  <filename>" — filename may have a leading "*"
		fn := strings.TrimPrefix(parts[len(parts)-1], "*")
		if strings.EqualFold(fn, wantLow) || strings.HasSuffix(strings.ToLower(fn), wantLow) {
			return strings.ToLower(parts[0])
		}
	}
	return ""
}

func (u *Updater) assetBaseName(tag string) string {
	return fmt.Sprintf("%s-%s%s", u.Repo, tag, u.AssetSuffix)
}

func extractBinary(zipBytes []byte, name string) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		return nil, err
	}
	for _, f := range zr.File {
		base := f.Name
		if i := strings.LastIndexAny(base, `/\`); i >= 0 {
			base = base[i+1:]
		}
		if strings.EqualFold(base, name) {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()
			return io.ReadAll(rc)
		}
	}
	return nil, fmt.Errorf("binary %q not found in zip", name)
}

// versionGreater returns latest > current using a permissive semver compare.
// Both inputs may be "vX.Y.Z" or "X.Y.Z"; non-numeric pre-release suffixes
// like "-rc1" are ignored for ordering (treated as equal to the release).
func versionGreater(latest, current string) bool {
	l := parseVersion(latest)
	c := parseVersion(current)
	for i := 0; i < 3; i++ {
		if l[i] > c[i] {
			return true
		}
		if l[i] < c[i] {
			return false
		}
	}
	return false
}

func parseVersion(s string) [3]int {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	if i := strings.IndexAny(s, "-+"); i >= 0 {
		s = s[:i]
	}
	parts := strings.SplitN(s, ".", 4)
	var out [3]int
	for i := 0; i < 3 && i < len(parts); i++ {
		out[i] = atoi(parts[i])
	}
	return out
}

func atoi(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return n
		}
		n = n*10 + int(r-'0')
	}
	return n
}
