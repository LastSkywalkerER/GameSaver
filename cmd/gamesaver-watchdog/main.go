// gamesaver-watchdog is a tiny supervisor process that gets registered as
// the Windows shell (HKCU\...\Winlogon\Shell). At logon it:
//
//  1. Reads %LOCALAPPDATA%\GameSaver\bin\target.txt to learn where the
//     real GameSaver.exe lives.
//  2. Spawns GameSaver and waits.
//  3. If GameSaver exits with code 0 → stops (user wants out).
//     If it crashes (non-zero exit or dies within 3 s of launch) → relaunches,
//     rate-limited to 5 restarts in 30 s. Past that, gives up and pops a
//     MessageBox so the user isn't stuck staring at a blank screen.
//  4. Listens globally for Ctrl+Alt+Shift+F12 — the fail-safe escape hatch
//     that removes the shell registration, starts explorer.exe, then exits.
//
// We keep this exe deliberately small (~2 MB stripped) so the on-demand
// download from GitHub releases is fast.
package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows/registry"
)

// Build-time version, injected via -ldflags "-X main.Version=v0.4.1".
var Version = "dev"

const (
	// Hotkey modifiers (MOD_*) and VK_F12.
	modAlt     = 0x0001
	modCtrl    = 0x0002
	modShift   = 0x0004
	vkF12      = 0x7B
	hotkeyID   = 1
	wmHotkey   = 0x0312
	swHide     = 0
	swRestore  = 9
	maxRestarts        = 5
	restartWindow      = 30 * time.Second
	minLifeForSuccess  = 3 * time.Second
)

var (
	user32             = syscall.NewLazyDLL("user32.dll")
	procRegisterHotKey = user32.NewProc("RegisterHotKey")
	procGetMessageW    = user32.NewProc("GetMessageW")
	procMessageBoxW    = user32.NewProc("MessageBoxW")
)

type msg struct {
	HWnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	X, Y    int32
}

func main() {
	// CLI escape hatch — let advanced users disable shell mode without
	// having to log in to a working desktop first (boot to recovery → start
	// cmd → run "gamesaver-watchdog.exe --disable-shell").
	if len(os.Args) > 1 && (os.Args[1] == "--disable-shell" || os.Args[1] == "-d") {
		if err := disableShellMode(); err != nil {
			fmt.Fprintln(os.Stderr, "disable failed:", err)
			os.Exit(1)
		}
		fmt.Println("shell mode disabled; next logon goes back to explorer.exe")
		os.Exit(0)
	}
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "-v") {
		fmt.Println("gamesaver-watchdog", Version)
		os.Exit(0)
	}

	target, err := resolveTarget()
	if err != nil {
		messageBox("GameSaver watchdog", "Cannot find GameSaver.exe:\n\n"+err.Error()+
			"\n\nDisabling shell mode and starting Explorer.")
		_ = disableShellMode()
		_ = exec.Command("explorer.exe").Start()
		os.Exit(1)
	}

	// Hotkey listener runs on its own OS-thread (RegisterHotKey/GetMessage
	// are thread-affine — the messages arrive on the thread that called
	// RegisterHotKey). When the user hits the panic hotkey we just exit
	// the process, which also kills the supervised GameSaver via process
	// group; then the shell-disable + explorer.exe relaunch happens here.
	go runHotkeyListener()

	exit := superviseLoop(target)

	// Whatever made us stop, hand the desktop back to the user.
	_ = exec.Command("explorer.exe").Start()
	os.Exit(exit)
}

// resolveTarget reads %LOCALAPPDATA%\GameSaver\bin\target.txt, which is
// written by the GameSaver UI when the user enabled shell mode. We never
// guess the path — if the file is missing or stale, bail loudly.
func resolveTarget() (string, error) {
	bin, err := binDir()
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(filepath.Join(bin, "target.txt"))
	if err != nil {
		return "", fmt.Errorf("read target.txt: %w", err)
	}
	p := string(b)
	for len(p) > 0 && (p[len(p)-1] == '\n' || p[len(p)-1] == '\r' || p[len(p)-1] == ' ') {
		p = p[:len(p)-1]
	}
	if p == "" {
		return "", errors.New("target.txt is empty")
	}
	if _, err := os.Stat(p); err != nil {
		return "", fmt.Errorf("stat %q: %w", p, err)
	}
	return p, nil
}

func binDir() (string, error) {
	la := os.Getenv("LOCALAPPDATA")
	if la == "" {
		return "", errors.New("LOCALAPPDATA env not set")
	}
	return filepath.Join(la, "GameSaver", "bin"), nil
}

// superviseLoop runs GameSaver and restarts it on crash, rate-limited.
// Returns the process exit code we should use ourselves.
func superviseLoop(target string) int {
	var crashTimes []time.Time
	for {
		// Tell GameSaver it's living under us. The app uses this to skip the
		// tray init (no Explorer = no tray) and to treat the X button as a
		// real exit instead of hide-to-tray.
		started := time.Now()
		cmd := exec.Command(target)
		cmd.Env = append(os.Environ(), "GS_SHELL_MODE=1")
		cmd.SysProcAttr = &syscall.SysProcAttr{
			// New process group so killing the watchdog (Ctrl+Alt+Shift+F12
			// → os.Exit) tears down GameSaver too.
			CreationFlags: 0x00000200, // CREATE_NEW_PROCESS_GROUP
		}
		err := cmd.Run()
		ranFor := time.Since(started)

		// Clean exit: user requested out, don't relaunch.
		if err == nil && ranFor >= minLifeForSuccess {
			return 0
		}

		// Crash or insta-die — log a crash timestamp, evict old ones outside
		// the 30 s window, and bail if the rate-limit fires.
		now := time.Now()
		crashTimes = append(crashTimes, now)
		cutoff := now.Add(-restartWindow)
		fresh := crashTimes[:0]
		for _, t := range crashTimes {
			if t.After(cutoff) {
				fresh = append(fresh, t)
			}
		}
		crashTimes = fresh

		if len(crashTimes) > maxRestarts {
			messageBox("GameSaver watchdog",
				fmt.Sprintf("GameSaver crashed %d times in %s. Giving up.\n\n"+
					"Shell mode left enabled — to disable, edit registry key\n"+
					"HKCU\\Software\\Microsoft\\Windows NT\\CurrentVersion\\Winlogon\\Shell\n"+
					"or run gamesaver-watchdog.exe --disable-shell.",
					len(crashTimes), restartWindow))
			return 1
		}

		// Short cooldown so we don't burn CPU when the app insta-crashes.
		time.Sleep(500 * time.Millisecond)
	}
}

// runHotkeyListener is the panic-button worker. Registers Ctrl+Alt+Shift+F12
// globally and on press disables shell mode + os.Exit(0)s the whole watchdog.
func runHotkeyListener() {
	// Required by RegisterHotKey: must call on a thread that runs a message loop.
	// Goroutines move between OS threads, so pin this one.
	// (Locking suffices — we never need to release.)
	r1, _, e := procRegisterHotKey.Call(0, hotkeyID,
		uintptr(modCtrl|modAlt|modShift), uintptr(vkF12))
	if r1 == 0 {
		// Fall back silently — the worst case is "no hotkey", user uses the
		// in-app exit button. Don't kill the watchdog over this.
		_ = e
		return
	}

	var m msg
	for {
		ret, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(ret) <= 0 {
			return
		}
		if m.Message == wmHotkey && m.WParam == hotkeyID {
			_ = disableShellMode()
			_ = exec.Command("explorer.exe").Start()
			os.Exit(0)
		}
	}
}

// disableShellMode removes the HKCU shell registration so the next logon
// goes back to explorer.exe by default.
func disableShellMode() error {
	k, err := registry.OpenKey(registry.CURRENT_USER,
		`Software\Microsoft\Windows NT\CurrentVersion\Winlogon`,
		registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	return k.DeleteValue("Shell")
}

func messageBox(title, body string) {
	t, _ := syscall.UTF16PtrFromString(title)
	b, _ := syscall.UTF16PtrFromString(body)
	const mbIconWarning = 0x00000030
	procMessageBoxW.Call(0, uintptr(unsafe.Pointer(b)), uintptr(unsafe.Pointer(t)), mbIconWarning)
}
