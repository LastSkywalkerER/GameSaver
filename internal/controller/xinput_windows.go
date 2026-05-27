// Package controller wraps the Win32 XInput API to detect an Xbox-style
// gamepad and stream button / d-pad / stick navigation events to the UI.
//
// We use xinput1_4.dll which ships with every Windows 10+ install — no
// external dependencies, no CGO, just a couple of syscall thunks.
//
// Frequency is 50 Hz (20 ms tick). That's the standard XInput polling rate
// for navigation use; games typically poll at 60-240 Hz but for menu nav
// 50 Hz is imperceptibly smooth and keeps wakeups tiny.
package controller

import (
	"context"
	"log/slog"
	"syscall"
	"time"
	"unsafe"
)

var (
	xinput       = syscall.NewLazyDLL("xinput1_4.dll")
	procGetState = xinput.NewProc("XInputGetState")
)

// XINPUT_GAMEPAD — see https://learn.microsoft.com/windows/win32/api/xinput/ns-xinput-xinput_gamepad
type gamepad struct {
	Buttons      uint16
	LeftTrigger  byte
	RightTrigger byte
	ThumbLX      int16
	ThumbLY      int16
	ThumbRX      int16
	ThumbRY      int16
}

// XINPUT_STATE
type state struct {
	PacketNumber uint32
	Gamepad      gamepad
}

// Button bit masks (XINPUT_GAMEPAD_*).
const (
	btnDPadUp    uint16 = 0x0001
	btnDPadDown  uint16 = 0x0002
	btnDPadLeft  uint16 = 0x0004
	btnDPadRight uint16 = 0x0008
	btnStart     uint16 = 0x0010
	btnBack      uint16 = 0x0020
	btnLB        uint16 = 0x0100
	btnRB        uint16 = 0x0200
	btnA         uint16 = 0x1000
	btnB         uint16 = 0x2000
	btnX         uint16 = 0x4000
	btnY         uint16 = 0x8000
)

// Press-only buttons (no auto-repeat) emitted as "controller:button".
// Action buttons + bumpers + start/back. D-pad and sticks go via "controller:nav"
// with auto-repeat semantics.
var buttonNames = []struct {
	bit  uint16
	name string
}{
	{btnA, "a"},
	{btnB, "b"},
	{btnX, "x"},
	{btnY, "y"},
	{btnStart, "start"},
	{btnBack, "back"},
	{btnLB, "lb"},
	{btnRB, "rb"},
}

const errSuccess = 0

// Service drives an XInput polling loop and emits UI events via the supplied
// emit callback (which is typically a thin wrapper around wailsruntime.EventsEmit).
type Service struct {
	emit func(string, any)
}

func New(emit func(string, any)) *Service {
	return &Service{emit: emit}
}

// Run blocks until ctx is cancelled. Polls XInput user-index 0 (first
// connected controller) and emits three event types:
//
//	"controller:state"  {connected: bool}                       — on edge change
//	"controller:button" {button: "a|b|x|y|start|back|lb|rb"}   — on press, no repeat
//	"controller:nav"    {dir:    "up|down|left|right"}         — d-pad or LS, with repeat
//
// Auto-repeat for directional input: first event fires immediately on press,
// then after a 350 ms initial delay the same direction repeats every 120 ms
// while held. Matches the rhythm of OS keyboard repeat — fast enough to
// scroll a long list, slow enough that one tap is one move.
func (s *Service) Run(ctx context.Context) {
	if err := procGetState.Find(); err != nil {
		slog.Info("xinput not available, controller support disabled", "err", err)
		return
	}

	const (
		tick          = 20 * time.Millisecond // 50 Hz
		deadzone      = int16(16000)          // ~50% of int16 range — chunky to ignore drift
		repeatInitial = 350 * time.Millisecond
		repeatPeriod  = 120 * time.Millisecond
	)

	ticker := time.NewTicker(tick)
	defer ticker.Stop()

	var (
		connected   bool
		prevButtons uint16
		navDir      string
		navStarted  time.Time
		lastRepeat  time.Time
	)

	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			var st state
			ret, _, _ := procGetState.Call(0, uintptr(unsafe.Pointer(&st)))

			if ret != errSuccess {
				if connected {
					connected = false
					s.emit("controller:state", map[string]any{"connected": false})
				}
				continue
			}
			if !connected {
				connected = true
				s.emit("controller:state", map[string]any{"connected": true})
				// Suppress button events on the first frame so a held button
				// from before connect doesn't fire spuriously.
				prevButtons = st.Gamepad.Buttons
				continue
			}

			// Edge-triggered button presses (current AND NOT previous).
			curr := st.Gamepad.Buttons
			pressed := curr & ^prevButtons
			for _, b := range buttonNames {
				if pressed&b.bit != 0 {
					s.emit("controller:button", map[string]any{"button": b.name})
				}
			}
			prevButtons = curr

			// Direction from d-pad OR left stick (whichever is active).
			// We use a single direction per tick — diagonals collapse to the
			// dominant axis. Fine for menu nav; would be wrong for game input.
			var dir string
			switch {
			case curr&btnDPadUp != 0 || st.Gamepad.ThumbLY > deadzone:
				dir = "up"
			case curr&btnDPadDown != 0 || st.Gamepad.ThumbLY < -deadzone:
				dir = "down"
			case curr&btnDPadLeft != 0 || st.Gamepad.ThumbLX < -deadzone:
				dir = "left"
			case curr&btnDPadRight != 0 || st.Gamepad.ThumbLX > deadzone:
				dir = "right"
			}

			if dir == "" {
				navDir = ""
				continue
			}
			if dir != navDir {
				// Fresh press in a new direction — emit immediately.
				s.emit("controller:nav", map[string]any{"dir": dir})
				navDir = dir
				navStarted = now
				lastRepeat = now
				continue
			}
			// Same direction held — wait for initial delay, then repeat.
			if now.Sub(navStarted) < repeatInitial {
				continue
			}
			if now.Sub(lastRepeat) >= repeatPeriod {
				s.emit("controller:nav", map[string]any{"dir": dir})
				lastRepeat = now
			}
		}
	}
}
