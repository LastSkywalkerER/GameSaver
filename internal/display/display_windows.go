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
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
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

// DEVMODEW — variable-tailed but the leading layout we need is fixed.
// Size is 220 bytes on modern Windows. We allocate that exact size so
// the union/padding offsets line up with what the Win32 ABI expects.
type devModeW struct {
	DeviceName    [32]uint16
	SpecVersion   uint16
	DriverVersion uint16
	Size          uint16
	DriverExtra   uint16
	Fields        uint32

	// Union: printer { 8 × int16 = 16 B } OR display { POINTL + 2 × DWORD = 16 B }
	PositionX           int32
	PositionY           int32
	DisplayOrientation  uint32
	DisplayFixedOutput  uint32

	Color       int16
	Duplex      int16
	YResolution int16
	TTOption    int16
	Collate     int16
	_pad0       int16
	FormName    [32]uint16
	LogPixels   uint16
	_pad1       uint16
	BitsPerPel  uint32
	PelsWidth   uint32
	PelsHeight  uint32
	DisplayFlags uint32
	DisplayFrequency uint32
	ICMMethod    uint32
	ICMIntent    uint32
	MediaType    uint32
	DitherType   uint32
	Reserved1    uint32
	Reserved2    uint32
	PanningWidth uint32
	PanningHeight uint32
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
func MakeSole(targetID string) error {
	current, err := List()
	if err != nil {
		return err
	}
	if len(current) == 0 {
		return errors.New("no displays found")
	}
	var found bool
	for _, m := range current {
		if m.ID == targetID {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("target %q not in active monitor list", targetID)
	}
	// Already the only one? Nothing to do.
	if len(current) == 1 {
		return nil
	}

	if err := saveBackup(current); err != nil {
		// Don't refuse — backup is nice-to-have, the user can still
		// re-enable via Windows display settings.
		_ = err
	}

	// Disable each non-target by zeroing its DEVMODE width/height/position
	// and pushing with CDS_UPDATEREGISTRY|CDS_NORESET. Then commit with a
	// single ChangeDisplaySettingsEx(NULL,…,0,…) to apply them all at once.
	for _, m := range current {
		if m.ID == targetID {
			continue
		}
		var dm devModeW
		dm.Size = uint16(unsafe.Sizeof(dm))
		dm.Fields = dmPosition | dmPelsWidth | dmPelsHeight
		// width/height/position already zero
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
	// Commit pending changes.
	r, _, _ := procChangeDisplaySetting.Call(0, 0, 0, 0, 0)
	if int32(r) != dispChangeSuccessful {
		return fmt.Errorf("commit: ChangeDisplaySettingsEx returned %d", int32(r))
	}
	return nil
}

// RestoreSaved re-enables monitors disabled by MakeSole using the saved
// snapshot. If there's no snapshot (no MakeSole was ever called, or it
// was already restored), returns nil silently.
func RestoreSaved() error {
	snap, ok, err := loadBackup()
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	for _, m := range snap {
		var dm devModeW
		dm.Size = uint16(unsafe.Sizeof(dm))
		dm.Fields = dmPosition | dmPelsWidth | dmPelsHeight
		dm.PositionX = int32(m.PositionX)
		dm.PositionY = int32(m.PositionY)
		dm.PelsWidth = uint32(m.Width)
		dm.PelsHeight = uint32(m.Height)
		idW, _ := syscall.UTF16PtrFromString(m.ID)
		r, _, _ := procChangeDisplaySetting.Call(
			uintptr(unsafe.Pointer(idW)),
			uintptr(unsafe.Pointer(&dm)),
			0,
			uintptr(cdsUpdateRegistry|cdsNoreset),
			0,
		)
		if int32(r) != dispChangeSuccessful {
			return fmt.Errorf("restore %s: ChangeDisplaySettingsEx returned %d", m.ID, int32(r))
		}
	}
	// Commit.
	procChangeDisplaySetting.Call(0, 0, 0, 0, 0)
	// Backup applied — remove it so a future MakeSole captures fresh state.
	_ = os.Remove(backupPath())
	return nil
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
