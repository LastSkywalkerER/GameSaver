// Package bluetooth wraps the Win32 BluetoothAPIs.dll calls we need
// for the in-shell Bluetooth picker — enumerate paired devices and
// connect/disconnect their audio services.
//
// The DLL exports we use:
//
//	BluetoothFindFirstDevice / BluetoothFindNextDevice / BluetoothFindDeviceClose
//	BluetoothSetServiceState
//
// All are documented in bluetoothapis.h. There is **no** way to
// "connect a paired device" generically — what Windows actually does
// is enable a particular *service* on the device (A2DP sink for
// headphone audio, HFP for the headset/mic, AVRCP for media keys,
// etc.). For audio we enumerate the common audio service GUIDs and
// enable / disable each one. Calls block until the radio responds —
// up to ~30 s if the device is out of range — so we always run them
// on a dedicated goroutine and let the frontend await.
//
// We deliberately don't handle PAIRING here. The user pairs new
// devices via Windows Settings; we only manage already-paired ones.
// Pairing requires BluetoothAuthenticateDevice + a PIN UI + a bunch
// of edge cases that are out of scope for the kiosk-mode picker.

package bluetooth

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ─── DLL imports ──────────────────────────────────────────────────────────

var (
	dll                          = syscall.NewLazyDLL("BluetoothApis.dll")
	procBluetoothFindFirstDevice = dll.NewProc("BluetoothFindFirstDevice")
	procBluetoothFindNextDevice  = dll.NewProc("BluetoothFindNextDevice")
	procBluetoothFindDeviceClose = dll.NewProc("BluetoothFindDeviceClose")
	procBluetoothSetServiceState = dll.NewProc("BluetoothSetServiceState")
	// Radio enumeration — needed to hand SetServiceState a real radio
	// handle (NULL is rejected on Win10/11, see setServicesByID).
	procBluetoothFindFirstRadio = dll.NewProc("BluetoothFindFirstRadio")
	procBluetoothFindRadioClose = dll.NewProc("BluetoothFindRadioClose")
)

// ─── Service GUIDs (Bluetooth SIG profile UUIDs) ──────────────────────────

// All BT profile GUIDs share the well-known base
// 0000xxxx-0000-1000-8000-00805F9B34FB. Service-specific bits in xxxx.
func svcGUID(short uint32) windows.GUID {
	return windows.GUID{
		Data1: short,
		Data2: 0x0000,
		Data3: 0x1000,
		Data4: [8]byte{0x80, 0x00, 0x00, 0x80, 0x5f, 0x9b, 0x34, 0xfb},
	}
}

// Audio-related profiles we toggle on connect/disconnect. We enable
// EVERY one of these on connect — different headphones surface
// themselves under different profile combinations and enabling an
// unsupported one is a clean no-op.
var (
	guidAudioSink           = svcGUID(0x110b) // A2DP playback target (headphones)
	guidAudioSource         = svcGUID(0x110a) // A2DP source (rarely matters here)
	guidAVRCPController     = svcGUID(0x110e) // media transport controls
	guidHandsfreeAudioGW    = svcGUID(0x1112) // HFP audio gateway
	guidHandsfree           = svcGUID(0x111e) // HFP client (headset mic)
	guidHeadset             = svcGUID(0x1108) // HSP (older voice profile)
)

var audioServiceGUIDs = []windows.GUID{
	guidAudioSink, guidAudioSource, guidAVRCPController,
	guidHandsfreeAudioGW, guidHandsfree, guidHeadset,
}

// ─── BTH struct layouts (byte-for-byte from bluetoothapis.h) ──────────────

// BLUETOOTH_FIND_RADIO_PARAMS. dwSize set to sizeof.
type btFindRadioParams struct {
	dwSize uint32
}

// BLUETOOTH_DEVICE_SEARCH_PARAMS. fields are BOOL (int32 in Win32 ABI).
type btDeviceSearchParams struct {
	dwSize                uint32
	fReturnAuthenticated  int32
	fReturnRemembered     int32
	fReturnUnknown        int32
	fReturnConnected      int32
	fIssueInquiry         int32
	cTimeoutMultiplier    uint8
	_pad                  [3]byte // align hRadio (HANDLE) to its natural 8 boundary
	hRadio                uintptr
}

// BLUETOOTH_DEVICE_INFO. szName is WCHAR[248]. SYSTEMTIME is 16 bytes
// (8 × uint16). Address is BLUETOOTH_ADDRESS which is a 6-byte MAC
// in a 8-byte union (the high 2 bytes are zero, but the field is
// still 8 bytes wide).
type btDeviceInfo struct {
	dwSize           uint32
	_                uint32 // pad to align ullAddress
	ullAddress       uint64 // BLUETOOTH_ADDRESS.ullLong
	ulClassOfDevice  uint32
	fConnected       int32
	fRemembered      int32
	fAuthenticated   int32
	stLastSeen       [8]uint16 // SYSTEMTIME
	stLastUsed       [8]uint16 // SYSTEMTIME
	szName           [248]uint16
}

// BLUETOOTH_DEVICE_INFO size on x64 must be 560 bytes per MSDN; the
// layout above gives us 4 + 4(pad) + 8 + 4 + 4*3 + 16 + 16 + 248*2 =
// 4+4+8+4+12+16+16+496 = 560 ✓.

// ─── Public API ───────────────────────────────────────────────────────────

// Device is the JSON-friendly shape returned to the frontend.
type Device struct {
	ID         string `json:"id"`         // 12-hex MAC (no separators) — stable identifier
	Address    string `json:"address"`    // human MAC "AA:BB:CC:DD:EE:FF"
	Name       string `json:"name"`
	Connected  bool   `json:"connected"`
	IsAudio    bool   `json:"isAudio"`    // major class 0x04 = Audio/Video
	ClassHex   string `json:"classHex"`   // raw class-of-device, useful for debugging
}

// runtimeMu serialises the slow blocking calls (SetServiceState can
// take 30 s while the radio negotiates). Multiple concurrent
// Connect/Disconnect requests would just queue inside the radio anyway;
// serialising means the frontend gets predictable error reporting.
var runtimeMu sync.Mutex

// List returns every PAIRED (remembered) bluetooth device. Unpaired
// devices (i.e. ones the user hasn't added in Windows Settings) are
// NOT returned — pairing is out of scope.
func List() ([]Device, error) {
	if err := procBluetoothFindFirstDevice.Find(); err != nil {
		return nil, fmt.Errorf("BluetoothApis.dll not available: %w", err)
	}

	// Pin OS thread so the HBLUETOOTH_DEVICE_FIND handle isn't yanked
	// out from under us by Go's scheduler between Find/Next calls.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	params := btDeviceSearchParams{
		dwSize:               uint32(unsafe.Sizeof(btDeviceSearchParams{})),
		fReturnAuthenticated: 1,
		fReturnRemembered:    1,
		fReturnUnknown:       0,
		fReturnConnected:     1,
		fIssueInquiry:        0, // don't trigger an active inquiry — we only want known/paired
		cTimeoutMultiplier:   2,
		hRadio:               0, // all radios
	}
	var info btDeviceInfo
	info.dwSize = uint32(unsafe.Sizeof(info))

	hFind, _, err := procBluetoothFindFirstDevice.Call(
		uintptr(unsafe.Pointer(&params)),
		uintptr(unsafe.Pointer(&info)),
	)
	if hFind == 0 {
		// ERROR_NO_MORE_ITEMS (259) is the documented "nothing paired" case.
		if errno, ok := err.(syscall.Errno); ok && errno == 259 {
			return []Device{}, nil
		}
		return nil, fmt.Errorf("BluetoothFindFirstDevice: %w", err)
	}
	defer procBluetoothFindDeviceClose.Call(hFind)

	out := []Device{toDevice(&info)}
	for {
		// Reset dwSize between iterations — BluetoothFindNextDevice reads it.
		info = btDeviceInfo{}
		info.dwSize = uint32(unsafe.Sizeof(info))
		r, _, err := procBluetoothFindNextDevice.Call(hFind, uintptr(unsafe.Pointer(&info)))
		if r == 0 {
			if errno, ok := err.(syscall.Errno); ok && errno == 259 {
				break
			}
			break // any other error: stop, return what we have
		}
		out = append(out, toDevice(&info))
	}
	return out, nil
}

// Connect best-effort enables ALL audio service profiles on the device.
// Returns the first hard error or nil if at least one service was
// flipped. Blocks until the radio negotiates — up to ~30 s if the
// device is asleep / out of range.
func Connect(deviceID string) error {
	return setServicesByID(deviceID, true)
}

// Disconnect flips the audio services off. Blocks similarly.
func Disconnect(deviceID string) error {
	return setServicesByID(deviceID, false)
}

const (
	btServiceDisable = 0x00
	btServiceEnable  = 0x01
)

func setServicesByID(deviceID string, enable bool) error {
	runtimeMu.Lock()
	defer runtimeMu.Unlock()

	// Locate the device by ID. We re-enumerate rather than caching:
	// btDeviceInfo carries the address we'll pass to SetServiceState,
	// and the user might have re-paired or renamed between calls.
	devices, err := listRaw()
	if err != nil {
		return err
	}
	var target *btDeviceInfo
	for i := range devices {
		if macHex(devices[i].ullAddress) == deviceID {
			target = &devices[i]
			break
		}
	}
	if target == nil {
		return fmt.Errorf("device %s not found (paired list refreshed)", deviceID)
	}

	flag := uintptr(btServiceDisable)
	if enable {
		flag = btServiceEnable
	}

	// BluetoothSetServiceState needs a REAL radio handle. Passing NULL
	// (what we did before) is rejected on Win10/11 with
	// ERROR_INVALID_PARAMETER (87): every service flip failed, no
	// connection was ever attempted — and the failure was invisible
	// because we printed GetLastError instead of the call's return code
	// (see below), so the user saw "errno 0: operation completed
	// successfully". Open the first local radio and pass its handle; if
	// there genuinely is no radio, fall back to NULL rather than refuse.
	radio, closeRadio := firstRadio()
	defer closeRadio()

	var firstErr error
	anyOK := false
	for i := range audioServiceGUIDs {
		g := audioServiceGUIDs[i]
		// BluetoothSetServiceState reports its result through the DWORD
		// RETURN VALUE (r), NOT GetLastError. The previous code read the
		// 3rd Call() return (GetLastError) — which is Errno(0) on the
		// no-SetLastError path, so a genuine failure formatted as the
		// nonsensical "errno 0: The operation completed successfully."
		// The error code is r.
		r, _, _ := procBluetoothSetServiceState.Call(
			radio,
			uintptr(unsafe.Pointer(target)),
			uintptr(unsafe.Pointer(&g)),
			flag,
		)
		if r == 0 { // ERROR_SUCCESS
			anyOK = true
			continue
		}
		// ERROR_SERVICE_DOES_NOT_EXIST (1060) / ERROR_NOT_FOUND (1168):
		// the device simply doesn't expose this profile — a clean skip,
		// not a real failure.
		if r == 1060 || r == 1168 {
			continue
		}
		if firstErr == nil {
			firstErr = fmt.Errorf("SetServiceState %04X failed (code %d): %v", g.Data1, r, syscall.Errno(r))
		}
	}
	if anyOK {
		return nil
	}
	if firstErr != nil {
		return firstErr
	}
	return errors.New("no audio services responded on this device")
}

// listRaw is List() but it returns the raw btDeviceInfo (we need the
// address bytes to pass back into SetServiceState).
func listRaw() ([]btDeviceInfo, error) {
	if err := procBluetoothFindFirstDevice.Find(); err != nil {
		return nil, err
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	params := btDeviceSearchParams{
		dwSize:               uint32(unsafe.Sizeof(btDeviceSearchParams{})),
		fReturnAuthenticated: 1, fReturnRemembered: 1, fReturnConnected: 1,
		cTimeoutMultiplier: 2,
	}
	var info btDeviceInfo
	info.dwSize = uint32(unsafe.Sizeof(info))
	hFind, _, err := procBluetoothFindFirstDevice.Call(
		uintptr(unsafe.Pointer(&params)),
		uintptr(unsafe.Pointer(&info)),
	)
	if hFind == 0 {
		if errno, ok := err.(syscall.Errno); ok && errno == 259 {
			return nil, nil
		}
		return nil, err
	}
	defer procBluetoothFindDeviceClose.Call(hFind)
	out := []btDeviceInfo{info}
	for {
		info = btDeviceInfo{}
		info.dwSize = uint32(unsafe.Sizeof(info))
		r, _, _ := procBluetoothFindNextDevice.Call(hFind, uintptr(unsafe.Pointer(&info)))
		if r == 0 {
			break
		}
		out = append(out, info)
	}
	return out, nil
}

// ─── Helpers ──────────────────────────────────────────────────────────────

// firstRadio opens a handle to the first local Bluetooth radio so
// BluetoothSetServiceState has a non-NULL hRadio to act on. The returned
// closeFn releases the radio HANDLE (CloseHandle) and the enumeration
// handle (BluetoothFindRadioClose). On any failure it returns (0, no-op)
// and the caller falls back to a NULL radio handle (best-effort).
func firstRadio() (uintptr, func()) {
	noop := func() {}
	if err := procBluetoothFindFirstRadio.Find(); err != nil {
		return 0, noop
	}
	var frp btFindRadioParams
	frp.dwSize = uint32(unsafe.Sizeof(frp))
	var h uintptr
	hFind, _, _ := procBluetoothFindFirstRadio.Call(
		uintptr(unsafe.Pointer(&frp)),
		uintptr(unsafe.Pointer(&h)),
	)
	if hFind == 0 || h == 0 {
		return 0, noop
	}
	return h, func() {
		procBluetoothFindRadioClose.Call(hFind)
		windows.CloseHandle(windows.Handle(h))
	}
}

func toDevice(info *btDeviceInfo) Device {
	name := windows.UTF16ToString(info.szName[:])
	if name == "" {
		name = "(unnamed)"
	}
	// Major device class (bits 8..12) — 0x04 = Audio/Video.
	major := (info.ulClassOfDevice >> 8) & 0x1F
	return Device{
		ID:        macHex(info.ullAddress),
		Address:   macColon(info.ullAddress),
		Name:      name,
		Connected: info.fConnected != 0,
		IsAudio:   major == 0x04,
		ClassHex:  fmt.Sprintf("0x%06X", info.ulClassOfDevice&0xFFFFFF),
	}
}

// BLUETOOTH_ADDRESS.ullLong holds the 6-byte MAC in the low 48 bits,
// little-endian (byte 0 = LSB). Render as the canonical big-endian
// "AA:BB:CC:DD:EE:FF" by reversing the byte order.
func macColon(ull uint64) string {
	b := macBytes(ull)
	return fmt.Sprintf("%02X:%02X:%02X:%02X:%02X:%02X", b[0], b[1], b[2], b[3], b[4], b[5])
}

// macHex returns the same MAC without separators — the stable ID we
// hand to the frontend.
func macHex(ull uint64) string {
	b := macBytes(ull)
	return fmt.Sprintf("%02X%02X%02X%02X%02X%02X", b[0], b[1], b[2], b[3], b[4], b[5])
}

func macBytes(ull uint64) [6]byte {
	return [6]byte{
		byte(ull >> 40), byte(ull >> 32), byte(ull >> 24),
		byte(ull >> 16), byte(ull >> 8),  byte(ull),
	}
}

