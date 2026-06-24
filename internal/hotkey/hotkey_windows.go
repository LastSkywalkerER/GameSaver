// Package hotkey registers ONE global Windows hotkey and calls a callback when
// it fires — even while a game owns the foreground. In shell mode it toggles
// between a running game and the launcher.
//
// We use RegisterHotKey (a supported, non-invasive API) rather than a
// WH_KEYBOARD_LL low-level keyboard hook: a global keyboard hook can be flagged
// by anti-cheat, and true Alt+Tab is OS-reserved and can't be registered
// anyway. Ctrl+Alt+Tab is the closest registrable combo to the muscle memory;
// if even that is already taken we fall back to Ctrl+Alt+` (backtick).
package hotkey

import (
	"log/slog"
	"runtime"
	"syscall"
	"unsafe"
)

var (
	user32                 = syscall.NewLazyDLL("user32.dll")
	kernel32               = syscall.NewLazyDLL("kernel32.dll")
	procRegisterHotKey     = user32.NewProc("RegisterHotKey")
	procUnregisterHotKey   = user32.NewProc("UnregisterHotKey")
	procGetMessageW        = user32.NewProc("GetMessageW")
	procPostThreadMessageW = user32.NewProc("PostThreadMessageW")
	procGetCurrentThreadID = kernel32.NewProc("GetCurrentThreadId")
)

const (
	modAlt      = 0x0001
	modControl  = 0x0002
	modNoRepeat = 0x4000 // collapse key auto-repeat into a single WM_HOTKEY (Win7+)

	wmQuit   = 0x0012
	wmHotkey = 0x0312

	vkTab  = 0x09
	vkOEM3 = 0xC0 // the `~` / backtick key on a US layout
)

// msg mirrors Win32 MSG (48 B on x64). We only read .message. Per the repo's
// Win32-ABI rule we do NOT add manual padding — Go's natural alignment inserts
// exactly the C padding (4 B after `message`, tail pad to 48).
type point struct{ x, y int32 }
type msg struct {
	hwnd    uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      point
}

type candidate struct {
	mod, vk uint32
	name    string
}

// Tried in order. Alt+Tab itself can't be registered (OS-reserved).
var candidates = []candidate{
	{modControl | modAlt | modNoRepeat, vkTab, "Ctrl+Alt+Tab"},
	{modControl | modAlt | modNoRepeat, vkOEM3, "Ctrl+Alt+`"},
}

// Service owns the hotkey registration + its dedicated message-loop thread.
type Service struct {
	onTrigger func()
	threadID  uint32
	done      chan struct{}
}

func New(onTrigger func()) *Service { return &Service{onTrigger: onTrigger} }

// Start registers the hotkey and runs a message loop on a dedicated OS thread.
// Returns the label of the combo that registered, or "" if none could be.
func (s *Service) Start() string {
	res := make(chan string, 1)
	s.done = make(chan struct{})
	go s.run(res)
	return <-res
}

func (s *Service) run(res chan<- string) {
	// RegisterHotKey binds to the calling thread and WM_HOTKEY is delivered to
	// THAT thread's message queue — so the loop must stay on the same locked
	// OS thread for the lifetime of the registration.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer close(s.done)

	tid, _, _ := procGetCurrentThreadID.Call()
	s.threadID = uint32(tid)

	const hotkeyID = 1
	var registered string
	for _, c := range candidates {
		r, _, err := procRegisterHotKey.Call(0, uintptr(hotkeyID), uintptr(c.mod), uintptr(c.vk))
		if r != 0 {
			registered = c.name
			break
		}
		slog.Warn("hotkey: RegisterHotKey failed", "combo", c.name, "err", err)
	}
	res <- registered
	if registered == "" {
		return
	}
	slog.Info("hotkey: registered", "combo", registered)

	var m msg
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		// GetMessageW returns >0 for a normal message, 0 for WM_QUIT, -1 on
		// error. Stop on anything ≤ 0.
		if int32(r) <= 0 {
			break
		}
		if m.message == wmHotkey && s.onTrigger != nil {
			s.onTrigger()
		}
	}
	procUnregisterHotKey.Call(0, uintptr(hotkeyID))
}

// Stop unblocks GetMessageW by posting WM_QUIT to the loop's thread, then waits
// for it to unregister and exit.
func (s *Service) Stop() {
	if s == nil || s.threadID == 0 {
		return
	}
	procPostThreadMessageW.Call(uintptr(s.threadID), wmQuit, 0, 0)
	<-s.done
}
