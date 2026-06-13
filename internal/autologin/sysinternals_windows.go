// Sysinternals Autologon support — an alternative to the netplwiz flow in
// autologin_windows.go.
//
// Why offer this in addition to netplwiz:
//   – The netplwiz checkbox is fragile across Windows builds: it's hidden on
//     Win11 22H2+ (we unhide it via a reg tweak) and Microsoft keeps moving
//     the user-accounts UI around. Sysinternals Autologon is a single, stable
//     Microsoft tool that has worked the same way for ~15 years.
//   – Like netplwiz, Autologon NEVER hands us the password: the user types it
//     into Autologon's own GUI and clicks Enable. Autologon stores it as an
//     encrypted LSA secret (same mechanism netplwiz uses), not plain-text in
//     the registry. (An admin can still decrypt LSA secrets — that's inherent
//     to autologon, documented by Microsoft, and the same for both flows.)
//
// We download AutoLogon.zip on demand (the official Microsoft URL), extract
// the architecture-appropriate exe into %LOCALAPPDATA%\GameSaver\bin\ — the
// same on-demand-binary pattern shellmode uses for the watchdog — and launch
// it elevated (Autologon needs admin to write LSA + HKLM\...\Winlogon). One
// UAC prompt; we stay unelevated.

package autologin

import (
	"archive/zip"
	"bytes"
	"context"
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
	// Official Sysinternals download. HTTPS to download.sysinternals.com
	// (Microsoft/Akamai) gives transport integrity; the extracted exe is also
	// Authenticode-signed by Microsoft, which UAC verifies at launch ("Verified
	// publisher: Microsoft Corporation"). So we don't pin a SHA — Sysinternals
	// ships no stable checksum endpoint and updates the tool periodically.
	autologonZipURL = "https://download.sysinternals.com/files/AutoLogon.zip"

	// Pre-accepting the EULA here suppresses the first-run nag dialog so the
	// flow is one click → straight to the configure window. Standard
	// Sysinternals mechanism: HKCU\Software\Sysinternals\<Tool>\EulaAccepted=1.
	autologonEulaKey = `Software\Sysinternals\Autologon`
)

// autologonCandidates lists the exe names inside AutoLogon.zip in preference
// order for this architecture. The zip ships Autologon.exe (x86),
// Autologon64.exe (x64) and Autologon64a.exe (ARM64).
func autologonCandidates() []string {
	switch runtime.GOARCH {
	case "arm64":
		return []string{"Autologon64a.exe", "Autologon64.exe", "Autologon.exe"}
	case "amd64":
		return []string{"Autologon64.exe", "Autologon.exe"}
	default: // 386
		return []string{"Autologon.exe"}
	}
}

func autologonBinDir() (string, error) {
	la := os.Getenv("LOCALAPPDATA")
	if la == "" {
		return "", errors.New("LOCALAPPDATA env not set")
	}
	return filepath.Join(la, "GameSaver", "bin"), nil
}

// autologonPath is the fixed on-disk location we install the arch-appropriate
// binary to. We always name it Autologon.exe regardless of which member of the
// zip we picked, so the "already downloaded?" check is a single stat.
func autologonPath() (string, error) {
	dir, err := autologonBinDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "Autologon.exe"), nil
}

// AutologonPresent reports whether the tool is already downloaded.
func AutologonPresent() bool {
	p, err := autologonPath()
	if err != nil {
		return false
	}
	st, err := os.Stat(p)
	return err == nil && !st.IsDir() && st.Size() > 0
}

// EnsureAutologon downloads + extracts Sysinternals Autologon for this
// architecture into %LOCALAPPDATA%\GameSaver\bin\Autologon.exe if it isn't
// already there. Returns the path it lives at. Blocks on the (~0.5 MB)
// download.
func EnsureAutologon(ctx context.Context) (string, error) {
	if runtime.GOOS != "windows" {
		return "", errors.New("autologon is Windows-only")
	}
	dst, err := autologonPath()
	if err != nil {
		return "", err
	}
	if AutologonPresent() {
		return dst, nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", fmt.Errorf("mkdir bin: %w", err)
	}
	zipBytes, err := downloadBytes(ctx, autologonZipURL)
	if err != nil {
		return "", fmt.Errorf("download Autologon: %w", err)
	}
	if err := extractAutologon(zipBytes, dst); err != nil {
		return "", err
	}
	return dst, nil
}

// OpenAutologon ensures the tool is present, pre-accepts the EULA, then
// launches it ELEVATED so the user can configure passwordless logon in
// Autologon's own GUI. We never pass credentials on the command line — the
// user types the password into the tool and clicks Enable; Windows stores it
// as an encrypted LSA secret. One UAC prompt (Autologon needs admin).
func OpenAutologon(ctx context.Context) error {
	exe, err := EnsureAutologon(ctx)
	if err != nil {
		return err
	}
	acceptAutologonEula() // best-effort, HKCU, no elevation needed
	// runElevated lives in autologin_windows.go. No args → Autologon shows its
	// GUI. If the user cancels the UAC prompt this returns ERROR_CANCELLED.
	return runElevated(exe, "")
}

func acceptAutologonEula() {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, autologonEulaKey, registry.SET_VALUE)
	if err != nil {
		return
	}
	defer k.Close()
	_ = k.SetDWordValue("EulaAccepted", 1)
}

// ─── download + unzip helpers ─────────────────────────────────────────────

func downloadBytes(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "GameSaver-autologin/1.0")
	client := &http.Client{Timeout: 2 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("http %d", resp.StatusCode)
	}
	// AutoLogon.zip is ~0.5 MB; cap the read so a misbehaving redirect to a
	// huge body can't exhaust memory.
	return io.ReadAll(io.LimitReader(resp.Body, 16<<20))
}

func extractAutologon(zipBytes []byte, dst string) error {
	zr, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		return fmt.Errorf("open AutoLogon.zip: %w", err)
	}
	// Index members by lowercased basename so arch lookup is case-insensitive.
	members := map[string]*zip.File{}
	for _, f := range zr.File {
		members[strings.ToLower(filepath.Base(f.Name))] = f
	}
	for _, name := range autologonCandidates() {
		if f, ok := members[strings.ToLower(name)]; ok {
			return writeZipMember(f, dst)
		}
	}
	return errors.New("no Autologon executable found inside AutoLogon.zip")
}

func writeZipMember(f *zip.File, dst string) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	// Write to a sibling .part then rename so a crash mid-download can't leave
	// a truncated exe that the present-check would treat as "installed".
	tmp := dst + ".part"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, io.LimitReader(rc, 64<<20)); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("install Autologon.exe: %w", err)
	}
	return nil
}
