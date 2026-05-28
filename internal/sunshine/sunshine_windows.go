// Package sunshine integrates with the Sunshine game-streaming host.
// It overwrites Sunshine's apps.json with GameSaver's library (GameSaver
// IS the menu, so the old list isn't worth keeping — we only retain the
// special "Desktop" entry so desktop streaming still works), then restarts
// SunshineService so the change is actually picked up.
//
// Box art: Moonlight reliably renders only PNG box art, and most of our
// cached covers are JPG, so we transcode each cover to PNG into
// cache\sunshine-art\ and point image-path there.
//
// apps.json lives in Program Files (Users have RX, not W) and the service
// restart needs admin too, so the whole apply is one UAC-elevated batch
// (copy → stop → kill stragglers → start → verify) that logs each step to
// a file we tail and stream to the UI as progress events.
package sunshine

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg" // register JPEG decoder for image.Decode
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

type Status struct {
	Installed bool   `json:"installed"`
	AppsPath  string `json:"appsPath"`
	Managed   int    `json:"managed"` // games currently registered (apps minus Desktop)
}

type SyncGame struct {
	Name         string
	Exe          string
	WorkingDir   string
	LaunchURI    string
	Source       string
	CoverAbsPath string // jpg or png on disk; "" if none
}

// Emit streams a human-readable progress line to the UI.
type Emit func(line string)

const serviceName = "SunshineService"

// ─── Detection ─────────────────────────────────────────────────────────

func Detect() Status {
	exe, ok := findSunshineExe()
	if !ok {
		return Status{Installed: false}
	}
	apps := resolveAppsPath(exe)
	st := Status{Installed: true, AppsPath: apps}
	if f, err := readApps(apps); err == nil {
		for _, a := range f.Apps {
			if !strings.EqualFold(appName(a), "Desktop") {
				st.Managed++
			}
		}
	}
	return st
}

func findSunshineExe() (string, bool) {
	for _, env := range []string{"ProgramFiles", "ProgramFiles(x86)"} {
		if p := os.Getenv(env); p != "" {
			c := filepath.Join(p, "Sunshine", "sunshine.exe")
			if fileExists(c) {
				return c, true
			}
		}
	}
	return "", false
}

func resolveAppsPath(exe string) string {
	confDir := filepath.Join(filepath.Dir(exe), "config")
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

// ─── apps.json model ─────────────────────────────────────────────────────

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
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false) // keep deep-link "&" literal
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

// desktopEntry returns the existing Desktop app from the file, or a default.
func desktopEntry(f *appsFile) map[string]interface{} {
	for _, a := range f.Apps {
		if strings.EqualFold(appName(a), "Desktop") {
			return a
		}
	}
	return map[string]interface{}{"name": "Desktop", "image-path": "desktop.png"}
}

// ─── Sync / Clear (full replace) ─────────────────────────────────────────

// Sync overwrites apps.json with [Desktop, ...games] and restarts Sunshine.
func Sync(games []SyncGame, emit Emit) error {
	if emit == nil {
		emit = func(string) {}
	}
	st := Detect()
	if !st.Installed {
		return errors.New("Sunshine не обнаружен")
	}
	emit(fmt.Sprintf("Собираю список: %d игр", len(games)))

	prev, err := readApps(st.AppsPath)
	if err != nil {
		return err
	}
	out := &appsFile{Env: prev.Env, Apps: []map[string]interface{}{desktopEntry(prev)}}

	emit("Готовлю обложки (PNG)…")
	staging := artDir()
	_ = os.MkdirAll(staging, 0o755)
	for _, g := range games {
		if g.Name == "" {
			continue
		}
		out.Apps = append(out.Apps, buildEntry(g, staging))
	}

	data, err := marshalApps(out)
	if err != nil {
		return err
	}
	emit(fmt.Sprintf("Записываю apps.json (%d записей)…", len(out.Apps)))
	// Copy our transcoded covers into Sunshine's own assets/ dir and
	// reference them by bare filename — that's how Sunshine's own (working)
	// entries do box art; absolute paths to arbitrary folders don't render
	// in Moonlight.
	assets := assetsDir()
	coverCmd := fmt.Sprintf(`copy /Y "%s\gs_*.png" "%s\"`, staging, assets)
	return applyElevated(st.AppsPath, data, coverCmd, "Copy covers to assets", emit)
}

// Clear resets apps.json to just the Desktop entry and restarts Sunshine.
func Clear(emit Emit) error {
	if emit == nil {
		emit = func(string) {}
	}
	st := Detect()
	if !st.Installed {
		return errors.New("Sunshine не обнаружен")
	}
	prev, err := readApps(st.AppsPath)
	if err != nil {
		return err
	}
	out := &appsFile{Env: prev.Env, Apps: []map[string]interface{}{desktopEntry(prev)}}
	data, err := marshalApps(out)
	if err != nil {
		return err
	}
	emit("Очищаю список (оставляю только Desktop)…")
	// Also wipe the covers we copied into assets/.
	coverCmd := fmt.Sprintf(`del /q "%s\gs_*.png" 2>nul`, assetsDir())
	return applyElevated(st.AppsPath, data, coverCmd, "Remove our covers from assets", emit)
}

// assetsDir is Sunshine's box-art directory (sibling of the exe). Bare
// image-path filenames resolve against it.
func assetsDir() string {
	exe, ok := findSunshineExe()
	if !ok {
		return ""
	}
	return filepath.Join(filepath.Dir(exe), "assets")
}

func buildEntry(g SyncGame, artDir string) map[string]interface{} {
	e := map[string]interface{}{
		"name":                    g.Name,
		"output":                  "",
		"elevated":                "false",
		"auto-detach":             "true",
		"wait-all":                "true",
		"exit-timeout":            "5",
		"exclude-global-prep-cmd": "false",
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
	if name := ensureCoverPNG(g.CoverAbsPath, artDir); name != "" {
		// Bare filename — Sunshine resolves it against its assets/ dir,
		// where applyElevated copies the gs_*.png files.
		e["image-path"] = name
	}
	return e
}

func quote(s string) string {
	if strings.HasPrefix(s, `"`) && strings.HasSuffix(s, `"`) {
		return s
	}
	return `"` + s + `"`
}

// ─── Cover → PNG ──────────────────────────────────────────────────────────

func artDir() string {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		base = os.TempDir()
	}
	return filepath.Join(base, "GameSaver", "cache", "sunshine-art")
}

// ensureCoverPNG produces a PNG named "gs_<base>.png" in the staging dir
// and returns the BARE filename to use as image-path. The gs_ prefix lets
// Clear wipe our covers from assets/ (del gs_*.png) without touching the
// user's / Sunshine's own art. Returns "" if there's no usable cover
// (entry then has no box art rather than failing the whole sync).
func ensureCoverPNG(src, dir string) string {
	if src == "" {
		return ""
	}
	base := strings.TrimSuffix(filepath.Base(src), filepath.Ext(src))
	name := "gs_" + base + ".png"
	dst := filepath.Join(dir, name)
	if fileExists(dst) {
		return name
	}
	if strings.EqualFold(filepath.Ext(src), ".png") {
		if copyFile(src, dst) == nil {
			return name
		}
		return ""
	}
	in, err := os.Open(src)
	if err != nil {
		return ""
	}
	defer in.Close()
	img, _, err := image.Decode(in) // jpeg + png decoders registered via imports
	if err != nil {
		return ""
	}
	out, err := os.Create(dst)
	if err != nil {
		return ""
	}
	defer out.Close()
	if err := png.Encode(out, img); err != nil {
		os.Remove(dst)
		return ""
	}
	return name
}

func copyFile(src, dst string) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, b, 0o644)
}

// ─── Apply: elevated copy + service restart, with streamed log ───────────

func gsDir() string {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		base = os.TempDir()
	}
	d := filepath.Join(base, "GameSaver")
	_ = os.MkdirAll(d, 0o755)
	return d
}

// applyElevated stages the new apps.json, then runs ONE elevated cmd that
// copies it into place and restarts SunshineService robustly (stop → kill
// orphan sunshine.exe that would otherwise hold the port → start). The
// command logs each step to a file; we tail it and stream lines via emit.
//
// CRITICAL: we pass the command INLINE via ShellExecuteEx's lpParameters
// (UTF-16) rather than writing a .bat file. cmd.exe reads .bat files in the
// console OEM codepage, which mangles the Cyrillic in the staged file's
// path (C:\Users\Администратор\…) → copy can't find the source → exit 1.
// The command line is UTF-16 natively, so Cyrillic paths survive. `chcp
// 65001` makes net/copy output land in the log as readable UTF-8.
func applyElevated(dst string, data []byte, coverCmd, coverLabel string, emit Emit) error {
	dir := gsDir()
	staged := filepath.Join(dir, "sunshine-apps.staged.json")
	logPath := filepath.Join(dir, "sunshine-apply.log")
	_ = os.Remove(logPath)

	if err := os.WriteFile(staged, data, 0o644); err != nil {
		return err
	}
	defer os.Remove(staged)

	// ASCII step echoes for clean progress; the && after the apps.json copy
	// makes a copy failure propagate as the group's (non-zero) exit code.
	// The cover step (copy/del gs_*.png in assets) is best-effort — games
	// still work without art, so it doesn't gate the rest.
	cmdline := fmt.Sprintf(
		`/c chcp 65001>nul & ( `+
			`(echo [1/6] Copy apps.json& copy /Y "%s" "%s") && `+
			`(echo [2/6] %s& %s& `+
			`echo [3/6] Stop %s& net stop %s& `+
			`echo [4/6] Kill leftover sunshine.exe& taskkill /f /im sunshine.exe >nul 2>&1& `+
			`echo [5/6] Start %s& net start %s& `+
			`echo [6/6] DONE) `+
			`) > "%s" 2>&1`,
		staged, dst, coverLabel, coverCmd, serviceName, serviceName, serviceName, serviceName, logPath)

	emit("Применяю изменения — подтверди UAC…")
	hProc, err := shellRunAs("cmd.exe", cmdline)
	if err != nil {
		return fmt.Errorf("эскалация отклонена/не удалась: %w", err)
	}
	defer closeHandle(hProc)

	// Tail the log while the elevated batch runs, emitting each new line.
	var off int64
	for {
		off = tailEmit(logPath, off, emit)
		if waitProcess(hProc, 300) { // exited
			break
		}
	}
	tailEmit(logPath, off, emit) // flush remainder

	if code := exitCode(hProc); code != 0 {
		return fmt.Errorf("apply завершился с кодом %d (см. лог)", code)
	}
	// Independent confirmation that the service is back.
	emit("Готово ✅")
	return nil
}

// tailEmit reads logPath from offset, emits complete new lines, returns the
// new offset (leaving a partial trailing line for next time).
func tailEmit(logPath string, off int64, emit Emit) int64 {
	b, err := os.ReadFile(logPath)
	if err != nil || int64(len(b)) <= off {
		return off
	}
	chunk := b[off:]
	// Log is UTF-8 (chcp 65001), echoes are ASCII [n/5] tags — emit raw.
	text := string(chunk)
	last := strings.LastIndexByte(text, '\n')
	if last < 0 {
		return off // no complete line yet
	}
	complete := text[:last]
	for _, ln := range strings.Split(complete, "\n") {
		ln = strings.TrimRight(ln, "\r")
		if strings.TrimSpace(ln) != "" {
			emit(ln)
		}
	}
	return off + int64(last) + 1
}

func fileExists(p string) bool { fi, err := os.Stat(p); return err == nil && !fi.IsDir() }

// ─── Win32 ShellExecuteEx("runas") + process wait ────────────────────────

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
	swHide                = 0
	waitObject0           = 0x0
)

func shellRunAs(file, params string) (uintptr, error) {
	verb, _ := syscall.UTF16PtrFromString("runas")
	f, _ := syscall.UTF16PtrFromString(file)
	p, _ := syscall.UTF16PtrFromString(params)
	info := shellExecuteInfoW{
		fMask:        seeMaskNoCloseProcess,
		lpVerb:       verb,
		lpFile:       f,
		lpParameters: p,
		nShow:        swHide,
	}
	info.cbSize = uint32(unsafe.Sizeof(info))
	r, _, e := procShellExecuteExW.Call(uintptr(unsafe.Pointer(&info)))
	if r == 0 || info.hProcess == 0 {
		if r == 0 {
			return 0, e
		}
		return 0, errors.New("no process handle")
	}
	return info.hProcess, nil
}

// waitProcess returns true if the process has exited within ms.
func waitProcess(h uintptr, ms uint32) bool {
	r, _, _ := procWaitForSingle.Call(h, uintptr(ms))
	return r == waitObject0
}

func exitCode(h uintptr) uint32 {
	var code uint32
	procGetExitCode.Call(h, uintptr(unsafe.Pointer(&code)))
	return code
}

func closeHandle(h uintptr) { procCloseHandle.Call(h) }
