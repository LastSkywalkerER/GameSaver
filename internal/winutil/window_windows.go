// Package winutil pokes our own top-level window via Win32 — specifically
// to span it across the whole virtual desktop (so the monitor picker shows
// on every physical screen, even if Windows thinks a powered-off monitor
// is still attached) and to snap it back onto a single monitor afterward.
//
// Wails' own WindowSetPosition is screen-relative and fights us across
// multi-monitor negative-coordinate layouts, so we drive SetWindowPos
// directly with absolute virtual-screen coordinates.
package winutil

import (
	"syscall"
	"unsafe"
)

var (
	user32               = syscall.NewLazyDLL("user32.dll")
	procFindWindowW      = user32.NewProc("FindWindowW")
	procSetWindowPos     = user32.NewProc("SetWindowPos")
	procGetSystemMetrics = user32.NewProc("GetSystemMetrics")
)

const (
	smXVirtualScreen  = 76
	smYVirtualScreen  = 77
	smCXVirtualScreen = 78
	smCYVirtualScreen = 79

	swpNoZOrder   = 0x0004
	swpNoActivate = 0x0010
	swpShowWindow = 0x0040
)

// i32 packs a (possibly negative) int into the low 32 bits of a uintptr so
// the Win32 C side reads it back as a signed `int`. Plain uintptr(x) on a
// negative value would set the high bits and the API would see garbage.
func i32(v int) uintptr { return uintptr(uint32(int32(v))) }

func findSelf() uintptr {
	// Wails sets the window title to "GameSaver" (see options.App.Title).
	title, _ := syscall.UTF16PtrFromString("GameSaver")
	h, _, _ := procFindWindowW.Call(0, uintptr(unsafe.Pointer(title)))
	return h
}

// VirtualScreen returns the bounding box of all monitors combined. x/y can
// be negative when a monitor sits left of / above the primary.
func VirtualScreen() (x, y, w, h int) {
	gx, _, _ := procGetSystemMetrics.Call(smXVirtualScreen)
	gy, _, _ := procGetSystemMetrics.Call(smYVirtualScreen)
	gw, _, _ := procGetSystemMetrics.Call(smCXVirtualScreen)
	gh, _, _ := procGetSystemMetrics.Call(smCYVirtualScreen)
	return int(int32(gx)), int(int32(gy)), int(int32(gw)), int(int32(gh))
}

// SpanVirtualScreen resizes/repositions our window to cover the entire
// virtual desktop. Returns the rect it applied so the caller can hand the
// origin to the frontend for per-monitor layout.
func SpanVirtualScreen() (x, y, w, h int) {
	x, y, w, h = VirtualScreen()
	if hwnd := findSelf(); hwnd != 0 {
		procSetWindowPos.Call(hwnd, 0, i32(x), i32(y), i32(w), i32(h), swpNoZOrder|swpShowWindow)
	}
	return
}

// MoveToRect places our window at an absolute rect (used to snap back onto
// a single monitor once the user has picked one).
func MoveToRect(x, y, w, h int) {
	if hwnd := findSelf(); hwnd != 0 {
		procSetWindowPos.Call(hwnd, 0, i32(x), i32(y), i32(w), i32(h), swpNoZOrder|swpShowWindow)
	}
}
