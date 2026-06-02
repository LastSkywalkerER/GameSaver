// Package audio enumerates Windows audio endpoints (output/input) and
// switches the system default, controller-driven from the shell UI.
//
// Backed by the documented MMDevice API (IMMDeviceEnumerator etc.) for the
// list/state, and by the **undocumented** IPolicyConfig COM interface for
// SetDefaultEndpoint — the same one NirSoft SoundVolumeView, SoundSwitch,
// AutoHotkey audio scripts etc. have used for ~15 years. Stable across
// Vista→Win11. CLSID/IID never published in headers but never changed
// either; if Microsoft ever rotates them, SetDefault returns a clean
// HRESULT error and the rest of the picker keeps working.
//
// All COM here is hand-rolled vtable indirection over `syscall.SyscallN`.
// No CGO. Each Wails-bound call does CoInitialize+LockOSThread on its own
// goroutine and CoUninitialize+UnlockOSThread on exit.
package audio

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ─── DLL imports ──────────────────────────────────────────────────────────

var (
	ole32                = syscall.NewLazyDLL("ole32.dll")
	procCoInitializeEx   = ole32.NewProc("CoInitializeEx")
	procCoUninitialize   = ole32.NewProc("CoUninitialize")
	procCoCreateInstance = ole32.NewProc("CoCreateInstance")
	procCoTaskMemFree    = ole32.NewProc("CoTaskMemFree")
	procPropVariantClear = ole32.NewProc("PropVariantClear")
)

// ─── GUIDs / IIDs ─────────────────────────────────────────────────────────

var (
	clsidMMDeviceEnumerator = windows.GUID{Data1: 0xBCDE0395, Data2: 0xE52F, Data3: 0x467C, Data4: [8]byte{0x8E, 0x3D, 0xC4, 0x57, 0x92, 0x91, 0x69, 0x2E}}
	iidIMMDeviceEnumerator  = windows.GUID{Data1: 0xA95664D2, Data2: 0x9614, Data3: 0x4F35, Data4: [8]byte{0xA7, 0x46, 0xDE, 0x8D, 0xB6, 0x36, 0x17, 0xE6}}

	// IPolicyConfigClient — the COM class implementing IPolicyConfig*.
	clsidPolicyConfigClient = windows.GUID{Data1: 0x870AF99C, Data2: 0x171D, Data3: 0x4F9E, Data4: [8]byte{0xAF, 0x0D, 0xE6, 0x3D, 0xF4, 0x0C, 0x2B, 0xC9}}

	// Two IPolicyConfig interface IIDs live in the wild — we try them in
	// order and use whichever the local Windows build exposes:
	//
	//   IPolicyConfigVista (Vista layout) — SetDefaultEndpoint at slot 12.
	//   IPolicyConfig      (Win7+ layout) — adds ResetDeviceFormat at slot
	//                                       5, sliding SetDefaultEndpoint
	//                                       to slot 13.
	//
	// v0.10.4 used only the Vista IID. CoCreateInstance returned
	// E_NOINTERFACE (0x80004002) for that one on some Win10/11 builds
	// (the user-visible "IPolicyConfig: CoCreateInstance hr=0x80004002"
	// toast in the screenshot). v0.10.5 falls back to the Win7+ variant
	// + slot 13 when the Vista one is missing.
	iidIPolicyConfigVista = windows.GUID{Data1: 0x568B9108, Data2: 0x44BF, Data3: 0x40B4, Data4: [8]byte{0x90, 0x06, 0x86, 0xAF, 0xE5, 0xB5, 0xA6, 0x20}}
	iidIPolicyConfigWin7  = windows.GUID{Data1: 0xF8679F50, Data2: 0x850A, Data3: 0x41CF, Data4: [8]byte{0x9C, 0x72, 0x43, 0x0F, 0x29, 0x02, 0x90, 0xC8}}
)

// ─── PROPERTYKEY + PROPVARIANT (only VT_LPWSTR — the one we use) ─────────

type propertyKey struct {
	fmtid windows.GUID
	pid   uint32
}

var pkeyFriendlyName = propertyKey{
	fmtid: windows.GUID{Data1: 0xa45c254e, Data2: 0xdf1c, Data3: 0x4efd, Data4: [8]byte{0x80, 0x20, 0x67, 0xd1, 0x46, 0xa8, 0x50, 0xe0}},
	pid:   14,
}

// propVariant matches the on-disk PROPVARIANT layout on 64-bit Windows
// (24 bytes). We only handle the VT_LPWSTR branch — val carries the LPWSTR.
type propVariant struct {
	vt         uint16
	wReserved1 uint16
	wReserved2 uint16
	wReserved3 uint16
	val        uintptr // LPWSTR when vt == 31
	_          uintptr // padding to 24 bytes
}

// ─── Constants ────────────────────────────────────────────────────────────

const (
	// EDataFlow
	eRender  = 0
	eCapture = 1

	// ERole
	eConsole       = 0
	eMultimedia    = 1
	eCommunications = 2

	// DEVICE_STATE
	deviceStateActive = 0x01

	coinitMultiThreaded = 0
	clsctxAll           = 0x17
	stgmRead            = 0
	vtLpwstr            = 31
)

// Vtable slots (0..2 are IUnknown.QueryInterface/AddRef/Release on every
// interface). The IPolicyConfigVista layout is the one used by SoundSwitch
// and every other Win7+ audio switcher — SetDefaultEndpoint is at 12, not
// 13 (which is the Vista IPolicyConfig — that one has an extra
// ResetDeviceFormat before SetDefaultEndpoint).
const (
	vtRelease = 2

	vtEnumAudioEndpoints      = 3
	vtGetDefaultAudioEndpoint = 4

	vtCollectionGetCount = 3
	vtCollectionItem     = 4

	vtDeviceOpenPropertyStore = 4
	vtDeviceGetId             = 5

	vtPSGetValue = 5

	// SetDefaultEndpoint slot depends on which IPolicyConfig variant we
	// got out of CoCreateInstance — 12 for the Vista layout, 13 for the
	// Win7+ layout (which has ResetDeviceFormat inserted at slot 5).
	// SetDefault picks the right one at runtime based on which IID
	// CoCreateInstance accepted.
	vtPolicySetDefaultEndpointVista = 12
	vtPolicySetDefaultEndpointWin7  = 13
)

// ─── Public API ───────────────────────────────────────────────────────────

// Device is what the UI lists. DataFlow is "render" (output) or "capture"
// (input). ID is the MMDevice endpoint instance ID — the same opaque
// string IPolicyConfig.SetDefaultEndpoint takes.
type Device struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	DataFlow  string `json:"dataFlow"`
	IsDefault bool   `json:"isDefault"`
}

// List returns all ACTIVE render + capture endpoints, with the current
// per-flow Multimedia default flagged.
func List() ([]Device, error) {
	if err := coInit(); err != nil {
		return nil, err
	}
	defer coUninit()

	enum, err := coCreateInstance(&clsidMMDeviceEnumerator, &iidIMMDeviceEnumerator)
	if err != nil {
		return nil, err
	}
	defer release(enum)

	var out []Device
	for _, df := range []int{eRender, eCapture} {
		flow := "render"
		if df == eCapture {
			flow = "capture"
		}
		defaultID := getDefaultID(enum, df)
		coll, ok := enumActive(enum, df)
		if !ok {
			continue
		}
		var count uint32
		callVTable(coll, vtCollectionGetCount, uintptr(unsafe.Pointer(&count)))
		for i := uint32(0); i < count; i++ {
			var dev uintptr
			if hr := callVTable(coll, vtCollectionItem, uintptr(i), uintptr(unsafe.Pointer(&dev))); int32(hr) < 0 || dev == 0 {
				continue
			}
			id, _ := deviceGetID(dev)
			name, _ := deviceGetName(dev)
			release(dev)
			if id == "" {
				continue
			}
			if name == "" {
				name = "(unnamed)"
			}
			out = append(out, Device{
				ID:        id,
				Name:      name,
				DataFlow:  flow,
				IsDefault: id == defaultID,
			})
		}
		release(coll)
	}
	return out, nil
}

// SetDefault makes deviceID the default endpoint for BOTH Console and
// Multimedia roles (what the "Set Default" button in the Windows Sound
// dialog does). Communications role is left alone unless the caller asks.
//
// Two IPolicyConfig IIDs live in the wild — Vista layout
// (SetDefaultEndpoint at slot 12) and Win7+ layout (slot 13). We try
// the Vista one first for backwards compat with the long history of
// audio-switcher tools that pinned to it; if Windows answers
// E_NOINTERFACE (0x80004002), fall back to the Win7+ IID + slot 13.
func SetDefault(deviceID string) error {
	if deviceID == "" {
		return errors.New("empty device id")
	}
	if err := coInit(); err != nil {
		return err
	}
	defer coUninit()

	policy, vtSet, err := acquirePolicyConfig()
	if err != nil {
		return err
	}
	defer release(policy)

	idPtr, err := windows.UTF16PtrFromString(deviceID)
	if err != nil {
		return err
	}
	for _, role := range []int{eConsole, eMultimedia} {
		if hr := callVTable(policy, vtSet,
			uintptr(unsafe.Pointer(idPtr)), uintptr(role)); int32(hr) < 0 {
			return fmt.Errorf("SetDefaultEndpoint (role %d) hr=0x%x", role, hr)
		}
	}
	return nil
}

// acquirePolicyConfig returns (interface, SetDefaultEndpoint vtable slot,
// error). Probes the Vista IID first, then the Win7+ IID; the matching
// slot rides along so the caller doesn't have to remember which layout
// it got.
//
// On E_NOINTERFACE (0x80004002) we keep going — that's the documented
// "wrong IID for this version of the COM class" signal. Other HRESULTs
// (REGDB_E_CLASSNOTREG / E_ACCESSDENIED / etc.) are surfaced as-is
// from the first call, since they apply to both attempts equally.
func acquirePolicyConfig() (uintptr, uintptr, error) {
	const eNoInterface = 0x80004002
	policy, err := coCreateInstance(&clsidPolicyConfigClient, &iidIPolicyConfigVista)
	if err == nil {
		return policy, vtPolicySetDefaultEndpointVista, nil
	}
	// "CoCreateInstance hr=0x80004002" → fall through to the Win7+ IID.
	// We sniff the string because coCreateInstance wraps the hr without
	// exposing it as a typed code; cheap and unambiguous.
	if strings.Contains(err.Error(), fmt.Sprintf("hr=0x%x", uint32(eNoInterface))) {
		policy, err2 := coCreateInstance(&clsidPolicyConfigClient, &iidIPolicyConfigWin7)
		if err2 == nil {
			return policy, vtPolicySetDefaultEndpointWin7, nil
		}
		return 0, 0, fmt.Errorf("IPolicyConfig: %w (and Win7 fallback: %v)", err, err2)
	}
	return 0, 0, fmt.Errorf("IPolicyConfig: %w", err)
}

// ─── Internals ────────────────────────────────────────────────────────────

func getDefaultID(enum uintptr, df int) string {
	var dev uintptr
	hr := callVTable(enum, vtGetDefaultAudioEndpoint, uintptr(df), uintptr(eMultimedia), uintptr(unsafe.Pointer(&dev)))
	if int32(hr) < 0 || dev == 0 {
		return ""
	}
	defer release(dev)
	id, _ := deviceGetID(dev)
	return id
}

func enumActive(enum uintptr, df int) (uintptr, bool) {
	var coll uintptr
	hr := callVTable(enum, vtEnumAudioEndpoints, uintptr(df), uintptr(deviceStateActive), uintptr(unsafe.Pointer(&coll)))
	if int32(hr) < 0 || coll == 0 {
		return 0, false
	}
	return coll, true
}

func deviceGetID(dev uintptr) (string, error) {
	var p uintptr
	hr := callVTable(dev, vtDeviceGetId, uintptr(unsafe.Pointer(&p)))
	if int32(hr) < 0 || p == 0 {
		return "", fmt.Errorf("GetId hr=0x%x", hr)
	}
	defer procCoTaskMemFree.Call(p)
	return windows.UTF16PtrToString((*uint16)(unsafe.Pointer(p))), nil
}

func deviceGetName(dev uintptr) (string, error) {
	var store uintptr
	hr := callVTable(dev, vtDeviceOpenPropertyStore, uintptr(stgmRead), uintptr(unsafe.Pointer(&store)))
	if int32(hr) < 0 || store == 0 {
		return "", fmt.Errorf("OpenPropertyStore hr=0x%x", hr)
	}
	defer release(store)

	var pv propVariant
	hr = callVTable(store, vtPSGetValue,
		uintptr(unsafe.Pointer(&pkeyFriendlyName)),
		uintptr(unsafe.Pointer(&pv)))
	if int32(hr) < 0 {
		return "", fmt.Errorf("GetValue hr=0x%x", hr)
	}
	defer procPropVariantClear.Call(uintptr(unsafe.Pointer(&pv)))
	if pv.vt != vtLpwstr || pv.val == 0 {
		return "", nil
	}
	return windows.UTF16PtrToString((*uint16)(unsafe.Pointer(pv.val))), nil
}

// ─── COM glue ────────────────────────────────────────────────────────────

func coInit() error {
	runtime.LockOSThread()
	r, _, _ := procCoInitializeEx.Call(0, coinitMultiThreaded)
	// S_OK=0, S_FALSE=1 (already initialised) — both fine.
	if int32(r) < 0 {
		runtime.UnlockOSThread()
		return fmt.Errorf("CoInitializeEx hr=0x%x", r)
	}
	return nil
}

func coUninit() {
	procCoUninitialize.Call()
	runtime.UnlockOSThread()
}

func coCreateInstance(clsid, iid *windows.GUID) (uintptr, error) {
	var ptr uintptr
	r, _, _ := procCoCreateInstance.Call(
		uintptr(unsafe.Pointer(clsid)),
		0,
		clsctxAll,
		uintptr(unsafe.Pointer(iid)),
		uintptr(unsafe.Pointer(&ptr)),
	)
	if int32(r) < 0 || ptr == 0 {
		return 0, fmt.Errorf("CoCreateInstance hr=0x%x", r)
	}
	return ptr, nil
}

func release(this uintptr) {
	if this != 0 {
		callVTable(this, vtRelease)
	}
}

// callVTable invokes COM method n in the vtable. Args MUST NOT include
// `this` — we prepend it.
func callVTable(this uintptr, n uintptr, args ...uintptr) uintptr {
	vtable := *(*uintptr)(unsafe.Pointer(this))
	method := *(*uintptr)(unsafe.Pointer(vtable + n*unsafe.Sizeof(uintptr(0))))
	full := append([]uintptr{this}, args...)
	r, _, _ := syscall.SyscallN(method, full...)
	return r
}
