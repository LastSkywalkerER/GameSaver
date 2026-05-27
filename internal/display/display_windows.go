// Package display wraps the Win32 display-config APIs to enumerate
// monitors, disable all-but-one, and roll back. Used by shell mode to
// let the user pick a single "console" monitor on logon while the
// others stay dark for the session.
//
// We use the classic EnumDisplayDevices / EnumDisplaySettingsEx /
// ChangeDisplaySettingsEx path rather than the newer
// QueryDisplayConfig / SetDisplayConfig because the former is simpler,
// has been stable since XP, and is enough for the "make exactly one
// active" use case.
package display

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

var (
	user32                   = syscall.NewLazyDLL("user32.dll")
	procEnumDisplayDevicesW  = user32.NewProc("EnumDisplayDevicesW")
	procEnumDisplaySettingsW = user32.NewProc("EnumDisplaySettingsExW")
	procChangeDisplaySetting = user32.NewProc("ChangeDisplaySettingsExW")
)

// ─── Win32 structs ─────────────────────────────────────────────────────

// DISPLAY_DEVICEW — size 840 bytes.
type displayDeviceW struct {
	cb           uint32
	DeviceName   [32]uint16
	DeviceString [128]uint16
	StateFlags   uint32
	DeviceID     [128]uint16
	DeviceKey    [128]uint16
}

// DEVMODEW — exact-match for the Win32 ABI layout. sizeof(DEVMODEW) is
// 220 bytes on modern Windows; mismatches by even one byte shift every
// field past the offending one, which silently turns PelsHeight reads
// into PelsWidth reads (and is rejected by ChangeDisplaySettingsEx as
// DISP_CHANGE_BADMODE = -2). The MSDN layout has NO padding between
// dmCollate/dmFormName or between dmLogPixels/dmBitsPerPel — Go's
// natural alignment of uint32 takes care of the LogPixels→BitsPerPel
// gap on its own, so we explicitly don't add filler shorts.
type devModeW struct {
	DeviceName    [32]uint16 // 0..63
	SpecVersion   uint16     // 64..65
	DriverVersion uint16     // 66..67
	Size          uint16     // 68..69
	DriverExtra   uint16     // 70..71
	Fields        uint32     // 72..75

	// Union: printer (8 × int16 = 16 B) OR display (POINTL + 2 × DWORD = 16 B)
	PositionX          int32  // 76..79
	PositionY          int32  // 80..83
	DisplayOrientation uint32 // 84..87
	DisplayFixedOutput uint32 // 88..91

	Color       int16 // 92..93
	Duplex      int16 // 94..95
	YResolution int16 // 96..97
	TTOption    int16 // 98..99
	Collate     int16 // 100..101
	// dmFormName immediately follows dmCollate at offset 102 — no
	// padding here in the canonical Win32 layout.
	FormName  [32]uint16 // 102..165
	LogPixels uint16     // 166..167
	// Go auto-pads here so BitsPerPel lands at 168 (4-byte aligned).
	BitsPerPel       uint32 // 168..171
	PelsWidth        uint32 // 172..175
	PelsHeight       uint32 // 176..179
	DisplayFlags     uint32 // 180..183
	DisplayFrequency uint32 // 184..187
	ICMMethod        uint32 // 188..191
	ICMIntent        uint32 // 192..195
	MediaType        uint32 // 196..199
	DitherType       uint32 // 200..203
	Reserved1        uint32 // 204..207
	Reserved2        uint32 // 208..211
	PanningWidth     uint32 // 212..215
	PanningHeight    uint32 // 216..219
}

const (
	// DISPLAY_DEVICE state flags
	displayDeviceAttachedToDesktop = 0x00000001
	displayDevicePrimary           = 0x00000004
	displayDeviceMirrored          = 0x00000008

	// DEVMODE.Fields
	dmPosition   = 0x00000020
	dmPelsWidth  = 0x00080000
	dmPelsHeight = 0x00100000

	enumCurrentSettings = uint32(0xFFFFFFFF)

	// ChangeDisplaySettingsExW flags
	cdsUpdateRegistry = 0x00000001
	cdsSetPrimary     = 0x00000010
	cdsNoreset        = 0x10000000
	cdsReset          = 0x40000000

	dispChangeSuccessful = 0
)

// ─── Public API ────────────────────────────────────────────────────────

// Monitor is the snapshot we return to the UI per attached display.
type Monitor struct {
	ID        string `json:"id"`        // \\.\DISPLAY1 etc — what ChangeDisplaySettings needs
	Name      string `json:"name"`      // friendly e.g. "Dell U2723QE"
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	PositionX int    `json:"positionX"`
	PositionY int    `json:"positionY"`
	IsPrimary bool   `json:"isPrimary"`
	IsEnabled bool   `json:"isEnabled"` // attached to the desktop (i.e. currently in use)
}

// List enumerates every attached display adapter and returns the live
// monitors (StateFlags & ATTACHED_TO_DESKTOP). Disabled / inactive
// adapters are skipped so the picker only ever offers real screens.
func List() ([]Monitor, error) {
	if err := procEnumDisplayDevicesW.Find(); err != nil {
		return nil, err
	}
	out := make([]Monitor, 0, 4)
	var dev displayDeviceW
	dev.cb = uint32(unsafe.Sizeof(dev))

	for i := uint32(0); ; i++ {
		// Reset cb each iteration — Windows clobbers it.
		dev.cb = uint32(unsafe.Sizeof(dev))
		r1, _, _ := procEnumDisplayDevicesW.Call(0, uintptr(i), uintptr(unsafe.Pointer(&dev)), 0)
		if r1 == 0 {
			break
		}
		if dev.StateFlags&displayDeviceMirrored != 0 {
			continue
		}
		enabled := dev.StateFlags&displayDeviceAttachedToDesktop != 0
		if !enabled {
			// Not in use right now — useful info, but for the picker we
			// only want active ones.
			continue
		}

		var dm devModeW
		dm.Size = uint16(unsafe.Sizeof(dm))
		r2, _, _ := procEnumDisplaySettingsW.Call(
			uintptr(unsafe.Pointer(&dev.DeviceName[0])),
			uintptr(enumCurrentSettings),
			uintptr(unsafe.Pointer(&dm)),
			0,
		)
		if r2 == 0 {
			continue
		}

		// Friendly name: enumerate the monitor child to grab its
		// "Generic PnP Monitor" / actual model string. We do a second
		// EnumDisplayDevices on the adapter as parent.
		var child displayDeviceW
		child.cb = uint32(unsafe.Sizeof(child))
		procEnumDisplayDevicesW.Call(uintptr(unsafe.Pointer(&dev.DeviceName[0])), 0, uintptr(unsafe.Pointer(&child)), 0)
		name := syscall.UTF16ToString(child.DeviceString[:])
		if name == "" {
			name = syscall.UTF16ToString(dev.DeviceString[:])
		}

		out = append(out, Monitor{
			ID:        syscall.UTF16ToString(dev.DeviceName[:]),
			Name:      name,
			Width:     int(dm.PelsWidth),
			Height:    int(dm.PelsHeight),
			PositionX: int(dm.PositionX),
			PositionY: int(dm.PositionY),
			IsPrimary: dev.StateFlags&displayDevicePrimary != 0,
			IsEnabled: enabled,
		})
	}
	return out, nil
}

// MakeSole disables every active monitor except the one whose ID matches
// `targetID`. The previous configuration is saved to disk so RestoreSaved()
// can put everything back later.
//
// Two subtleties Windows insists on:
//  1. You can't disable the current primary while it IS the current
//     primary — Windows returns DISP_CHANGE_BADMODE (-2). So we first
//     promote the target to (0,0) + CDS_SET_PRIMARY, then disable the
//     rest.
//  2. The "disable" magic itself is DM_PELSWIDTH|DM_PELSHEIGHT both = 0,
//     plus DM_POSITION = (0,0). The whole batch is pushed with
//     CDS_NORESET, then committed with a final ChangeDisplaySettingsEx
//     (NULL,...,0,...) so everything happens atomically.
func MakeSole(targetID string) error {
	current, err := List()
	if err != nil {
		return err
	}
	if len(current) == 0 {
		return errors.New("no displays found")
	}
	var target *Monitor
	for i := range current {
		if current[i].ID == targetID {
			target = &current[i]
			break
		}
	}
	if target == nil {
		return fmt.Errorf("target %q not in active monitor list", targetID)
	}
	if len(current) == 1 {
		return nil
	}

	if err := saveBackup(current); err != nil {
		_ = err
	}

	// 1. Promote target to primary at (0,0). Need to send the FULL existing
	// DEVMODE so Windows keeps its resolution/refresh; we layer position
	// and the SET_PRIMARY flag on top.
	{
		var dm devModeW
		dm.Size = uint16(unsafe.Sizeof(dm))
		// Pre-fill with current settings so width/height/refresh are correct.
		idW, _ := syscall.UTF16PtrFromString(target.ID)
		procEnumDisplaySettingsW.Call(
			uintptr(unsafe.Pointer(idW)),
			uintptr(enumCurrentSettings),
			uintptr(unsafe.Pointer(&dm)),
			0,
		)
		dm.PositionX = 0
		dm.PositionY = 0
		dm.Fields |= dmPosition | dmPelsWidth | dmPelsHeight
		r, _, _ := procChangeDisplaySetting.Call(
			uintptr(unsafe.Pointer(idW)),
			uintptr(unsafe.Pointer(&dm)),
			0,
			uintptr(cdsSetPrimary|cdsUpdateRegistry|cdsNoreset),
			0,
		)
		if int32(r) != dispChangeSuccessful {
			return fmt.Errorf("set primary %s: ChangeDisplaySettingsEx returned %d", target.ID, int32(r))
		}
	}

	// 2. Disable each non-target by zeroing its DEVMODE.
	for _, m := range current {
		if m.ID == targetID {
			continue
		}
		var dm devModeW
		dm.Size = uint16(unsafe.Sizeof(dm))
		dm.Fields = dmPosition | dmPelsWidth | dmPelsHeight
		idW, _ := syscall.UTF16PtrFromString(m.ID)
		r, _, _ := procChangeDisplaySetting.Call(
			uintptr(unsafe.Pointer(idW)),
			uintptr(unsafe.Pointer(&dm)),
			0,
			uintptr(cdsUpdateRegistry|cdsNoreset),
			0,
		)
		if int32(r) != dispChangeSuccessful {
			return fmt.Errorf("disable %s: ChangeDisplaySettingsEx returned %d", m.ID, int32(r))
		}
	}

	// 3. Commit pending changes.
	r, _, _ := procChangeDisplaySetting.Call(0, 0, 0, 0, 0)
	if int32(r) != dispChangeSuccessful {
		return fmt.Errorf("commit: ChangeDisplaySettingsEx returned %d", int32(r))
	}
	return nil
}

// RestoreSaved re-enables monitors disabled by MakeSole using the saved
// snapshot. We put the original primary back FIRST (with CDS_SET_PRIMARY)
// so the other monitors can attach to a non-primary slot without Windows
// complaining about the topology. Returns nil silently if there's no
// snapshot.
func RestoreSaved() error {
	snap, ok, err := loadBackup()
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}

	apply := func(m Monitor, makePrimary bool) error {
		var dm devModeW
		dm.Size = uint16(unsafe.Sizeof(dm))
		dm.Fields = dmPosition | dmPelsWidth | dmPelsHeight
		dm.PositionX = int32(m.PositionX)
		dm.PositionY = int32(m.PositionY)
		dm.PelsWidth = uint32(m.Width)
		dm.PelsHeight = uint32(m.Height)
		flags := uint32(cdsUpdateRegistry | cdsNoreset)
		if makePrimary {
			flags |= cdsSetPrimary
		}
		idW, _ := syscall.UTF16PtrFromString(m.ID)
		r, _, _ := procChangeDisplaySetting.Call(
			uintptr(unsafe.Pointer(idW)),
			uintptr(unsafe.Pointer(&dm)),
			0,
			uintptr(flags),
			0,
		)
		if int32(r) != dispChangeSuccessful {
			return fmt.Errorf("restore %s: ChangeDisplaySettingsEx returned %d", m.ID, int32(r))
		}
		return nil
	}

	// Pass 1: the original primary.
	for _, m := range snap {
		if m.IsPrimary {
			if err := apply(m, true); err != nil {
				return err
			}
		}
	}
	// Pass 2: everything else.
	for _, m := range snap {
		if !m.IsPrimary {
			if err := apply(m, false); err != nil {
				return err
			}
		}
	}

	procChangeDisplaySetting.Call(0, 0, 0, 0, 0)
	_ = os.Remove(backupPath())
	return nil
}

// Watch polls List() at a low frequency and emits "display:changed" via the
// supplied emit callback whenever the topology changes (count of monitors
// or any monitor's ID/resolution/position changes). Cheap — five seconds
// between polls and List() takes a millisecond. We avoid hooking
// WM_DISPLAYCHANGE because Wails doesn't expose the window's WndProc and
// adding a sidecar message window for one event isn't worth the cost.
func Watch(ctx context.Context, emit func(string, any)) {
	var prev string
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			ms, err := List()
			if err != nil {
				continue
			}
			fp := fingerprint(ms)
			if prev == "" {
				// Establish baseline on first tick — don't emit a spurious
				// "changed" the moment the watcher starts up.
				prev = fp
				continue
			}
			if fp != prev {
				prev = fp
				emit("display:changed", ms)
			}
		}
	}
}

func fingerprint(ms []Monitor) string {
	parts := make([]string, len(ms))
	for i, m := range ms {
		parts[i] = fmt.Sprintf("%s|%dx%d@%d,%d", m.ID, m.Width, m.Height, m.PositionX, m.PositionY)
	}
	return strings.Join(parts, ";")
}

// ─── Backup persistence ────────────────────────────────────────────────

func backupPath() string {
	la := os.Getenv("LOCALAPPDATA")
	if la == "" {
		la = os.TempDir()
	}
	return filepath.Join(la, "GameSaver", "display-backup.json")
}

func saveBackup(s []Monitor) error {
	p := backupPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, b, 0o644)
}

func loadBackup() ([]Monitor, bool, error) {
	p := backupPath()
	b, err := os.ReadFile(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, err
	}
	var s []Monitor
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, false, err
	}
	return s, true, nil
}
