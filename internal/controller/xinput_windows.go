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
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

// We probe multiple XInput DLL versions and pick the first one that
// successfully returns state for *any* slot. v0.10.4: this exists
// because the Moonlight virtual gamepad (and some other ViGEm-style
// virtual controllers) doesn't always show up via xinput1_4 — only
// after Steam Input "wakes" the slot. The older 1_3 / 9_1_0 DLLs are
// shims over the same underlying driver but some virtual-pad drivers
// only respond to one of them.
//
// xinput1_3.dll also exports XInputGetStateEx at ordinal 100 — same
// XINPUT_STATE struct but the Buttons word also includes the Guide
// button (mask 0x0400). That's not exported by name on any version.
// We prefer Ex when available because some virtual pads have been
// reported to respond to it but not to the plain XInputGetState.

type xinputBackend struct {
	dll  *syscall.LazyDLL
	proc *syscall.LazyProc
	name string // for logging / debugging
}

var xinputBackends []xinputBackend

func init() {
	// Order matters: try ordinal-100 GetStateEx first (Guide button +
	// catches some virtual pads), then 1_4's named GetState (canonical),
	// then 1_3 / 9_1_0 fallbacks.
	xi13 := syscall.NewLazyDLL("xinput1_3.dll")
	if proc := xi13.NewProc("#100"); proc.Find() == nil {
		xinputBackends = append(xinputBackends, xinputBackend{xi13, proc, "1_3:#100(Ex)"})
	}
	xi14 := syscall.NewLazyDLL("xinput1_4.dll")
	if proc := xi14.NewProc("XInputGetState"); proc.Find() == nil {
		xinputBackends = append(xinputBackends, xinputBackend{xi14, proc, "1_4:GetState"})
	}
	if proc := xi13.NewProc("XInputGetState"); proc.Find() == nil {
		xinputBackends = append(xinputBackends, xinputBackend{xi13, proc, "1_3:GetState"})
	}
	xi910 := syscall.NewLazyDLL("xinput9_1_0.dll")
	if proc := xi910.NewProc("XInputGetState"); proc.Find() == nil {
		xinputBackends = append(xinputBackends, xinputBackend{xi910, proc, "9_1_0:GetState"})
	}
}

// procGetState retained as a thin compatibility marker so the
// `Find()` check on Run() startup keeps working — it just looks for
// the first available backend.
type procWrapper struct{}

func (procWrapper) Find() error {
	if len(xinputBackends) == 0 {
		return errAllXInputMissing
	}
	return nil
}

var errAllXInputMissing = &xinputErr{"no XInput DLL available (1_4 / 1_3 / 9_1_0 all missing)"}

type xinputErr struct{ msg string }

func (e *xinputErr) Error() string { return e.msg }

var procGetState = procWrapper{}

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
// Also exposes the latest connected state so the UI can query it on mount —
// otherwise a frontend that subscribes via EventsOn *after* the initial
// connect event has fired ends up stuck on "no pad" until the controller
// disconnects and reconnects.
type Service struct {
	emit      func(string, any)
	connected atomic.Bool
	paused    atomic.Bool
}

func New(emit func(string, any)) *Service {
	return &Service{emit: emit}
}

// IsConnected returns the most-recent poll result.
func (s *Service) IsConnected() bool { return s.connected.Load() }

// SetPaused stops/resumes emitting nav + button events. Used while a game
// is running (GameSaver minimised) so we don't drive the hidden UI behind
// the game. State (connected) keeps updating; only input emission pauses.
func (s *Service) SetPaused(p bool) { s.paused.Store(p) }

// Run blocks until ctx is cancelled. Polls XInput across ALL FOUR user
// indices (Windows assigns connected controllers to slots 0–3 — usually
// the first connected one lands at 0, but if another Bluetooth/XInput
// device is also paired it can sit on 1+, in which case polling only
// slot 0 misses it entirely). Emits three event types:
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
	slog.Info("xinput poller started", "backends", len(xinputBackends))

	const (
		tick          = 20 * time.Millisecond // 50 Hz
		deadzone      = int16(16000)          // ~50% of int16 range — chunky to ignore drift
		repeatInitial = 350 * time.Millisecond
		repeatPeriod  = 120 * time.Millisecond
	)

	// pollAny returns the first (backend, slot) pair that comes back
	// with ERROR_SUCCESS. We try every registered backend (1_4 / 1_3 /
	// 9_1_0, with ordinal-100 GetStateEx first) across all four user
	// slots. Sticky on the (backend, slot) pair until it disconnects so
	// two-controller flutter doesn't bounce focus, and so a virtual pad
	// that only responds to one backend stays attached even if a real
	// XInput pad is also plugged in.
	activeSlot := uint32(0xFFFF) // sentinel: none yet
	activeBackend := -1
	pollAny := func(out *state) (uint32, bool) {
		// Stick with our current (backend, slot) when possible.
		if activeSlot != 0xFFFF && activeBackend >= 0 && activeBackend < len(xinputBackends) {
			be := xinputBackends[activeBackend]
			r, _, _ := be.proc.Call(uintptr(activeSlot), uintptr(unsafe.Pointer(out)))
			if r == errSuccess {
				return activeSlot, true
			}
		}
		for bi, be := range xinputBackends {
			for i := uint32(0); i < 4; i++ {
				if bi == activeBackend && i == activeSlot {
					continue
				}
				r, _, _ := be.proc.Call(uintptr(i), uintptr(unsafe.Pointer(out)))
				if r == errSuccess {
					slog.Info("controller detected", "slot", i, "backend", be.name)
					activeBackend = bi
					return i, true
				}
			}
		}
		return 0xFFFF, false
	}

	ticker := time.NewTicker(tick)
	defer ticker.Stop()

	var (
		prevButtons uint16
		navDir      string
		navStarted  time.Time
		lastRepeat  time.Time
		wasPaused   bool
	)

	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			// Paused while a game is running (we minimised ourselves).
			// CRUCIAL: we don't even call XInputGetState here. Polling the
			// device at 50 Hz from the background contends with the game's
			// own XInput reads — the driver serialises access to a wireless
			// pad and the game ends up "missing" button presses. Backing off
			// the device entirely hands the controller cleanly to the game.
			if s.paused.Load() {
				wasPaused = true
				continue
			}

			var st state
			slot, ok := pollAny(&st)

			connected := s.connected.Load()
			if !ok {
				if connected {
					s.connected.Store(false)
					activeSlot = 0xFFFF
					activeBackend = -1
					s.emit("controller:state", map[string]any{"connected": false})
				}
				continue
			}
			activeSlot = slot
			if !connected {
				s.connected.Store(true)
				s.emit("controller:state", map[string]any{"connected": true})
				// Suppress button events on the first frame so a held button
				// from before connect doesn't fire spuriously.
				prevButtons = st.Gamepad.Buttons
				wasPaused = false
				continue
			}

			// First frame after resuming from pause — reset the button/nav
			// baseline so a button held during the game doesn't replay into
			// the carousel the moment we come back.
			if wasPaused {
				wasPaused = false
				prevButtons = st.Gamepad.Buttons
				navDir = ""
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

			// Right-stick → continuous "scroll" events for whatever the
			// UI considers its active scrollable surface (Settings page,
			// drawer body, backups modal). Smaller deadzone than nav
			// because we want fine, low-speed scrolling at the edges.
			// Emitted at the polling tick rate (50 Hz) — the UI receives
			// roughly 50 events per second of held stick, each with the
			// normalised magnitude in [-1.0..+1.0] for the direction.
			//
			// Note: emit only when outside the deadzone — the bulk of
			// idle ticks should not produce events (otherwise we'd
			// flood the event bus while the user just holds the pad).
			const rsDeadzone = int16(8000)
			rx := st.Gamepad.ThumbRX
			ry := st.Gamepad.ThumbRY
			if rx > rsDeadzone || rx < -rsDeadzone || ry > rsDeadzone || ry < -rsDeadzone {
				var dx, dy float64
				if rx > rsDeadzone || rx < -rsDeadzone {
					dx = float64(rx) / 32768.0
				}
				if ry > rsDeadzone || ry < -rsDeadzone {
					// XInput Y is UP-positive, but in the DOM "scroll
					// down" is +y, so flip the sign here. The frontend
					// can pass dy directly to el.scrollBy({top: dy}).
					dy = -float64(ry) / 32768.0
				}
				s.emit("controller:scroll", map[string]any{"dx": dx, "dy": dy})
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
