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
	// IPolicyConfigVista — Win7+ variant; on Win10/11 it's still the one to use.
	iidIPolicyConfigVista = windows.GUID{Data1: 0x568B9108, Data2: 0x44BF, Data3: 0x40B4, Data4: [8]byte{0x90, 0x06, 0x86, 0xAF, 0xE5, 0xB5, 0xA6, 0x20}}
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

	vtPolicySetDefaultEndpoint = 12
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
func SetDefault(deviceID string) error {
	if deviceID == "" {
		return errors.New("empty device id")
	}
	if err := coInit(); err != nil {
		return err
	}
	defer coUninit()

	policy, err := coCreateInstance(&clsidPolicyConfigClient, &iidIPolicyConfigVista)
	if err != nil {
		return fmt.Errorf("IPolicyConfig: %w", err)
	}
	defer release(policy)

	idPtr, err := windows.UTF16PtrFromString(deviceID)
	if err != nil {
		return err
	}
	for _, role := range []int{eConsole, eMultimedia} {
		if hr := callVTable(policy, vtPolicySetDefaultEndpoint,
			uintptr(unsafe.Pointer(idPtr)), uintptr(role)); int32(hr) < 0 {
			return fmt.Errorf("SetDefaultEndpoint (role %d) hr=0x%x", role, hr)
		}
	}
	return nil
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
