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
	"encoding/json"
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

type ghRelease struct {
	TagName     string    `json:"tag_name"`
	HTMLURL     string    `json:"html_url"`
	Body        string    `json:"body"`
	PublishedAt time.Time `json:"published_at"`
	Draft       bool      `json:"draft"`
	Prerelease  bool      `json:"prerelease"`
	Assets      []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
		Size               int64  `json:"size"`
	} `json:"assets"`
}

// Check queries GitHub's "latest release" endpoint. Returns Available=true
// only when there's a newer (and not skipped-by-user) semver tag with a
// platform asset published.
func (u *Updater) Check(ctx context.Context) (*UpdateInfo, error) {
	if u.CurrentVersion == "dev" || u.CurrentVersion == "" {
		return &UpdateInfo{Available: false, CurrentVer: u.CurrentVersion}, nil
	}
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", u.Owner, u.Repo)
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	// GitHub rejects API requests without a User-Agent (403). Be explicit
	// rather than rely on Go's default UA, which GitHub may also throttle.
	req.Header.Set("User-Agent", "GameSaver-updater")
	resp, err := u.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("github releases api: %s", resp.Status)
	}
	var rel ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, err
	}
	info := &UpdateInfo{
		CurrentVer:   u.CurrentVersion,
		LatestVer:    rel.TagName,
		ReleaseURL:   rel.HTMLURL,
		ReleaseNotes: rel.Body,
		PublishedAt:  rel.PublishedAt.Format(time.RFC3339),
	}
	if rel.Draft || rel.Prerelease || rel.TagName == "" {
		return info, nil
	}
	if !versionGreater(rel.TagName, u.CurrentVersion) {
		return info, nil
	}
	// Find an asset whose name ends with our platform suffix.
	for _, a := range rel.Assets {
		if strings.HasSuffix(a.Name, u.AssetSuffix) {
			info.AssetURL = a.BrowserDownloadURL
			info.AssetSize = a.Size
			break
		}
	}
	// Pull SHA-256 from the checksums.txt asset, if published.
	for _, a := range rel.Assets {
		if a.Name == "checksums.txt" {
			info.SHA256 = u.fetchSha(ctx, a.BrowserDownloadURL, u.assetBaseName(rel.TagName))
			break
		}
	}
	info.Available = info.AssetURL != ""
	return info, nil
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
