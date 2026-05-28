// Package sunshine integrates with the Sunshine game-streaming host
// (https://github.com/LizardByte/Sunshine). It can sync GameSaver's
// library into Sunshine's apps.json (so the games show up in Moonlight
// with cover art) and remove the entries it added.
//
// Sunshine's apps.json is the registry of streamable apps. Each entry:
//
//	{
//	  "name": "Cyberpunk 2077",
//	  "cmd": "\"H:\\Games\\...\\Cyberpunk2077.exe\"",   // or a steam:// / epic deep-link
//	  "working-dir": "\"H:\\Games\\...\"",
//	  "image-path": "<abs path or filename under assets/>",
//	  "auto-detach": "true", "wait-all": "true", "exit-timeout": "5", ...
//	}
//
// On a default Windows install apps.json lives under
// %ProgramFiles%\Sunshine\config\, which is NOT user-writable — so the
// write goes through a one-shot UAC-elevated copy (see writeElevated).
//
// We never clobber the user's own entries (Desktop, Steam Big Picture,
// anything they added by hand): we track the names WE added in a sidecar
// file and only ever add/refresh/remove those.
package sunshine

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

// Status is the snapshot the Settings UI polls.
type Status struct {
	Installed bool   `json:"installed"`           // sunshine.exe found
	AppsPath  string `json:"appsPath"`            // resolved apps.json path
	Managed   int    `json:"managed"`             // entries we currently manage
	NeedsAdmin bool  `json:"needsAdmin"`          // apps.json isn't user-writable → UAC on sync
}

// SyncGame is the minimal per-game input the caller (app.go) builds from a
// GameView. CoverAbsPath is an absolute path to a cover image on disk (or "").
type SyncGame struct {
	Name         string
	Exe          string
	WorkingDir   string
	LaunchURI    string
	Source       string
	CoverAbsPath string
}

// ─── Detection ─────────────────────────────────────────────────────────

// Detect locates the Sunshine install + its apps.json. Installed is true if
// sunshine.exe is found; AppsPath is the best-guess apps.json location even
// if it doesn't exist yet.
func Detect() Status {
	exe, ok := findSunshineExe()
	if !ok {
		return Status{Installed: false}
	}
	apps := resolveAppsPath(exe)
	st := Status{Installed: true, AppsPath: apps}
	st.NeedsAdmin = !pathWritable(apps)
	if names, err := loadManaged(); err == nil {
		st.Managed = len(names)
	}
	return st
}

func findSunshineExe() (string, bool) {
	cands := []string{}
	for _, env := range []string{"ProgramFiles", "ProgramFiles(x86)"} {
		if p := os.Getenv(env); p != "" {
			cands = append(cands, filepath.Join(p, "Sunshine", "sunshine.exe"))
		}
	}
	for _, c := range cands {
		if fi, err := os.Stat(c); err == nil && !fi.IsDir() {
			return c, true
		}
	}
	return "", false
}

// resolveAppsPath returns where apps.json should be. Order:
//  1. file_apps override in sunshine.conf, if set
//  2. <install>\config\apps.json (legacy / portable — this machine)
//  3. %PROGRAMDATA%\Sunshine\config\apps.json (newer service installs)
func resolveAppsPath(exe string) string {
	installDir := filepath.Dir(exe)
	confDir := filepath.Join(installDir, "config")

	if conf := filepath.Join(confDir, "sunshine.conf"); fileExists(conf) {
		if p := readConfFileApps(conf); p != "" {
			if !filepath.IsAbs(p) {
				p = filepath.Join(confDir, p)
			}
			return p
		}
	}
	if legacy := filepath.Join(confDir, "apps.json"); fileExists(legacy) {
		return legacy
	}
	if pd := os.Getenv("PROGRAMDATA"); pd != "" {
		if newer := filepath.Join(pd, "Sunshine", "config", "apps.json"); fileExists(newer) {
			return newer
		}
	}
	// Default to the legacy location even if not present yet.
	return filepath.Join(confDir, "apps.json")
}

func readConfFileApps(conf string) string {
	b, err := os.ReadFile(conf)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "file_apps") {
			if i := strings.Index(line, "="); i >= 0 {
				return strings.Trim(strings.TrimSpace(line[i+1:]), `"`)
			}
		}
	}
	return ""
}

// ─── apps.json model (preserves unknown fields of user entries) ──────────

type appsFile struct {
	Env  json.RawMessage          `json:"env"`
	Apps []map[string]interface{} `json:"apps"`
}

func readApps(path string) (*appsFile, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &appsFile{Env: json.RawMessage(`""`)}, nil
		}
		return nil, err
	}
	var f appsFile
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("parse apps.json: %w", err)
	}
	if len(f.Env) == 0 {
		f.Env = json.RawMessage(`""`)
	}
	return &f, nil
}

func marshalApps(f *appsFile) ([]byte, error) {
	// SetEscapeHTML(false) so deep-link URIs keep their literal "&"
	// (?action=launch&silent=true) instead of "&".
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "    ")
	if err := enc.Encode(f); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func appName(a map[string]interface{}) string {
	if n, ok := a["name"].(string); ok {
		return n
	}
	return ""
}

// ─── Sync / Clear ────────────────────────────────────────────────────────

// Sync upserts every game into apps.json (by name) and records the names
// in the managed sidecar. User-authored entries are left untouched.
// Returns how many GameSaver entries are now present.
func Sync(games []SyncGame) (int, error) {
	st := Detect()
	if !st.Installed {
		return 0, errors.New("Sunshine not detected")
	}
	f, err := readApps(st.AppsPath)
	if err != nil {
		return 0, err
	}

	prev, _ := loadManaged()
	cur := make([]string, 0, len(games))
	curSet := map[string]bool{}
	for _, g := range games {
		if g.Name == "" {
			continue
		}
		cur = append(cur, g.Name)
		curSet[g.Name] = true
	}
	remove := map[string]bool{}
	for _, n := range prev {
		remove[n] = true
	}
	for n := range curSet {
		remove[n] = true
	}

	// Keep everything that isn't ours-now / ours-before.
	kept := f.Apps[:0]
	for _, a := range f.Apps {
		if !remove[appName(a)] {
			kept = append(kept, a)
		}
	}
	f.Apps = kept
	for _, g := range games {
		if g.Name == "" {
			continue
		}
		f.Apps = append(f.Apps, buildEntry(g))
	}

	data, err := marshalApps(f)
	if err != nil {
		return 0, err
	}
	if err := writeApps(st.AppsPath, data, st.NeedsAdmin); err != nil {
		return 0, err
	}
	_ = saveManaged(cur)
	return len(cur), nil
}

// Clear removes only the entries GameSaver added (per the sidecar) and
// empties the sidecar. Returns how many were removed.
func Clear() (int, error) {
	st := Detect()
	if !st.Installed {
		return 0, errors.New("Sunshine not detected")
	}
	prev, _ := loadManaged()
	if len(prev) == 0 {
		return 0, nil
	}
	remove := map[string]bool{}
	for _, n := range prev {
		remove[n] = true
	}
	f, err := readApps(st.AppsPath)
	if err != nil {
		return 0, err
	}
	before := len(f.Apps)
	kept := f.Apps[:0]
	for _, a := range f.Apps {
		if !remove[appName(a)] {
			kept = append(kept, a)
		}
	}
	f.Apps = kept
	removed := before - len(f.Apps)

	data, err := marshalApps(f)
	if err != nil {
		return 0, err
	}
	if err := writeApps(st.AppsPath, data, st.NeedsAdmin); err != nil {
		return 0, err
	}
	_ = saveManaged(nil)
	return removed, nil
}

// buildEntry mirrors internal/launcher's priority: Steam prefers its
// deep-link; everyone else prefers the bare exe; deep-link only as a
// fallback. auto-detach=true so Sunshine returns to the stream after the
// launcher hands off (matches how the user's existing entries are set).
func buildEntry(g SyncGame) map[string]interface{} {
	e := map[string]interface{}{
		"name":                    g.Name,
		"output":                  "",
		"elevated":                "false",
		"auto-detach":             "true",
		"wait-all":                "true",
		"exit-timeout":            "5",
		"exclude-global-prep-cmd": "false",
		// Best-effort marker; the sidecar is the real source of truth since
		// Sunshine's Web UI may drop unknown fields on its own saves.
		"gamesaver-managed": "true",
	}
	switch {
	case g.Source == "steam" && g.LaunchURI != "":
		e["cmd"] = g.LaunchURI
	case g.Exe != "":
		e["cmd"] = quote(g.Exe)
		if g.WorkingDir != "" {
			e["working-dir"] = quote(g.WorkingDir)
		}
	case g.LaunchURI != "":
		e["cmd"] = g.LaunchURI
	}
	if g.CoverAbsPath != "" {
		e["image-path"] = g.CoverAbsPath
	}
	return e
}

func quote(s string) string {
	if strings.HasPrefix(s, `"`) && strings.HasSuffix(s, `"`) {
		return s
	}
	return `"` + s + `"`
}

// ─── Managed-names sidecar (%LOCALAPPDATA%\GameSaver\sunshine-managed.json) ─

func managedPath() string {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		base = os.TempDir()
	}
	return filepath.Join(base, "GameSaver", "sunshine-managed.json")
}

func loadManaged() ([]string, error) {
	b, err := os.ReadFile(managedPath())
	if err != nil {
		return nil, err
	}
	var names []string
	if err := json.Unmarshal(b, &names); err != nil {
		return nil, err
	}
	return names, nil
}

func saveManaged(names []string) error {
	p := managedPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(names, "", "  ")
	return os.WriteFile(p, b, 0o644)
}

// ─── Writing apps.json (direct, or UAC-elevated copy) ────────────────────

func writeApps(path string, data []byte, _ bool) error {
	// We ALWAYS go through the elevated path, even if the file itself were
	// user-writable: Sunshine keeps its app list in memory and only re-reads
	// apps.json on (re)start, so the change is invisible to Moonlight until
	// we restart SunshineService — and that needs admin regardless. So one
	// UAC prompt does both: copy the staged file into place AND bounce the
	// service so Sunshine reloads.
	tmp := filepath.Join(filepath.Dir(managedPath()), "sunshine-apps.staged.json")
	if err := os.MkdirAll(filepath.Dir(tmp), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	defer os.Remove(tmp)
	return installAndReload(tmp, path)
}

func fileExists(p string) bool { fi, err := os.Stat(p); return err == nil && !fi.IsDir() }

// pathWritable probes whether we can write `path` (or create it in its dir)
// without elevation.
func pathWritable(path string) bool {
	if fileExists(path) {
		f, err := os.OpenFile(path, os.O_WRONLY, 0)
		if err == nil {
			f.Close()
			return true
		}
		return false
	}
	// Try creating a temp file in the target dir.
	probe := filepath.Join(filepath.Dir(path), ".gs-write-probe")
	f, err := os.OpenFile(probe, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return false
	}
	f.Close()
	os.Remove(probe)
	return true
}

// ─── ShellExecuteEx("runas") elevated copy ───────────────────────────────

var (
	shell32             = syscall.NewLazyDLL("shell32.dll")
	procShellExecuteExW = shell32.NewProc("ShellExecuteExW")
	kernel32            = syscall.NewLazyDLL("kernel32.dll")
	procWaitForSingle   = kernel32.NewProc("WaitForSingleObject")
	procGetExitCode     = kernel32.NewProc("GetExitCodeProcess")
	procCloseHandle     = kernel32.NewProc("CloseHandle")
)

type shellExecuteInfoW struct {
	cbSize         uint32
	fMask          uint32
	hwnd           uintptr
	lpVerb         *uint16
	lpFile         *uint16
	lpParameters   *uint16
	lpDirectory    *uint16
	nShow          int32
	hInstApp       uintptr
	lpIDList       uintptr
	lpClass        *uint16
	hkeyClass      uintptr
	dwHotKey       uint32
	hIconOrMonitor uintptr
	hProcess       uintptr
}

const (
	seeMaskNoCloseProcess = 0x00000040
	seeMaskNoAsync        = 0x00000100
	swHide                = 0
	waitInfinite          = 0xFFFFFFFF
)

// installAndReload copies src→dst AND restarts SunshineService, both in a
// single elevated `cmd /c` (one UAC prompt). Sunshine caches its app list,
// so without the restart Moonlight never sees the new apps.json.
//
// cmd structure: the copy is grouped and gated with `&&` so a copy failure
// is detected via exit code; the restart is best-effort (a non-service
// Sunshine install has nothing to stop) and `ver >nul` forces the group's
// exit code to 0 so a restart hiccup doesn't look like a write failure.
//
// We wait on the process + check the exit code so a cancelled UAC or a
// failed copy doesn't get reported as success.
func installAndReload(src, dst string) error {
	verb, _ := syscall.UTF16PtrFromString("runas")
	file, _ := syscall.UTF16PtrFromString("cmd.exe")
	params, _ := syscall.UTF16PtrFromString(fmt.Sprintf(
		`/c (copy /Y "%s" "%s") && (net stop SunshineService & net start SunshineService & ver >nul)`,
		src, dst))

	info := shellExecuteInfoW{
		fMask:        seeMaskNoCloseProcess | seeMaskNoAsync,
		lpVerb:       verb,
		lpFile:       file,
		lpParameters: params,
		nShow:        swHide,
	}
	info.cbSize = uint32(unsafe.Sizeof(info))
	r, _, e := procShellExecuteExW.Call(uintptr(unsafe.Pointer(&info)))
	if r == 0 {
		return fmt.Errorf("elevation declined or failed: %w", e)
	}
	if info.hProcess == 0 {
		return nil // launched, no handle to wait on
	}
	defer procCloseHandle.Call(info.hProcess)
	procWaitForSingle.Call(info.hProcess, uintptr(waitInfinite))
	var code uint32
	procGetExitCode.Call(info.hProcess, uintptr(unsafe.Pointer(&code)))
	if code != 0 {
		return fmt.Errorf("elevated copy failed (exit %d)", code)
	}
	return nil
}
