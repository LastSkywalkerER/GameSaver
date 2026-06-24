// Package bluetooth wraps the Win32 BluetoothAPIs.dll calls we need for the
// in-shell Bluetooth picker: discover nearby devices (active inquiry), pair a
// new device, and connect/disconnect a paired device's audio services — all
// from inside the controller-driven kiosk UI, without the Windows "Add a
// device" wizard (which also lists printers / network devices and needs a
// mouse).
//
// The DLL exports we use:
//
//	BluetoothFindFirstDevice / BluetoothFindNextDevice / BluetoothFindDeviceClose
//	BluetoothSetServiceState
//	BluetoothFindFirstRadio / BluetoothFindRadioClose
//	BluetoothAuthenticateDeviceEx / BluetoothRegisterForAuthenticationEx /
//	BluetoothSendAuthenticationResponseEx   (pairing)
//
// "Connecting" a paired device is not a generic op — Windows enables a
// particular *service* (A2DP sink for headphone audio, HFP for the mic, AVRCP
// for media keys). For audio we enable the common audio service GUIDs. These
// calls block until the radio responds (up to ~30 s if the device is asleep),
// so callers run them on a goroutine and the frontend awaits.
//
// CLASSIC ONLY: BluetoothFindFirstDevice's inquiry finds classic / dual-mode
// (BR/EDR) devices — which is what gaming headsets are. Pure-BLE peripherals
// need WinRT's DeviceWatcher (no Go bindings here); for those the picker keeps
// a "via Windows" fallback button.

package bluetooth

import (
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
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
	// handle (NULL is rejected on Win10/11, see setServicesLocked).
	procBluetoothFindFirstRadio = dll.NewProc("BluetoothFindFirstRadio")
	procBluetoothFindRadioClose = dll.NewProc("BluetoothFindRadioClose")
	// Pairing.
	procBluetoothAuthenticateDeviceEx         = dll.NewProc("BluetoothAuthenticateDeviceEx")
	procBluetoothRegisterForAuthenticationEx  = dll.NewProc("BluetoothRegisterForAuthenticationEx")
	procBluetoothSendAuthenticationResponseEx = dll.NewProc("BluetoothSendAuthenticationResponseEx")
	procBluetoothUnregisterAuthentication     = dll.NewProc("BluetoothUnregisterAuthentication")
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

// Audio-related profiles we toggle on connect/disconnect. We enable EVERY one
// of these on connect — different headphones surface themselves under
// different profile combinations and enabling an unsupported one is a clean
// no-op (ERROR_SERVICE_DOES_NOT_EXIST).
var (
	guidAudioSink        = svcGUID(0x110b) // A2DP playback target (headphones)
	guidAudioSource      = svcGUID(0x110a) // A2DP source (rarely matters here)
	guidAVRCPController  = svcGUID(0x110e) // media transport controls
	guidHandsfreeAudioGW = svcGUID(0x1112) // HFP audio gateway
	guidHandsfree        = svcGUID(0x111e) // HFP client (headset mic)
	guidHeadset          = svcGUID(0x1108) // HSP (older voice profile)
)

var audioServiceGUIDs = []windows.GUID{
	guidAudioSink, guidAudioSource, guidAVRCPController,
	guidHandsfreeAudioGW, guidHandsfree, guidHeadset,
}

// ─── Win32 error codes we branch on ───────────────────────────────────────

const (
	errSuccess              = 0
	errInvalidParameter     = 87
	errNoMoreItems          = 259
	waitTimeout             = 258
	errServiceDoesNotExist  = 1060
	errDeviceNotConnected   = 1167
	errNotFound             = 1168
	errNotAuthenticated     = 1244
	errSessionCredConflict  = 1219
)

// ─── BTH struct layouts (byte-for-byte from bluetoothapis.h) ──────────────
//
// 🔴 These sizes are asserted in bluetooth_windows_test.go. A one-byte drift
// silently shifts every field after the mistake (the devModeW 224-vs-220
// incident). Never add "safety" padding — Go's natural alignment already
// inserts exactly the C padding.

// BLUETOOTH_FIND_RADIO_PARAMS — 4 B.
type btFindRadioParams struct {
	dwSize uint32
}

// BLUETOOTH_DEVICE_SEARCH_PARAMS — 40 B on x64 (the HANDLE forces the struct
// to 8-alignment, so it's padded out to 40 even though the named fields total
// 36). dwSize must be set to the full 40.
type btDeviceSearchParams struct {
	dwSize               uint32
	fReturnAuthenticated int32
	fReturnRemembered    int32
	fReturnUnknown       int32
	fReturnConnected     int32
	fIssueInquiry        int32
	cTimeoutMultiplier   uint8
	_pad                 [3]byte // keep cTimeoutMultiplier's byte explicit; Go pads to 8 for hRadio
	hRadio               uintptr
}

// BLUETOOTH_DEVICE_INFO — 560 B on x64. szName is WCHAR[248]; SYSTEMTIME is 16
// bytes (8 × uint16); Address is BLUETOOTH_ADDRESS (6-byte MAC in an 8-byte
// union). 4+4+8+4+4*3+16+16+248*2 = 560.
type btDeviceInfo struct {
	dwSize          uint32
	_               uint32 // pad to align ullAddress
	ullAddress      uint64 // BLUETOOTH_ADDRESS.ullLong
	ulClassOfDevice uint32
	fConnected      int32
	fRemembered     int32
	fAuthenticated  int32
	stLastSeen      [8]uint16 // SYSTEMTIME
	stLastUsed      [8]uint16 // SYSTEMTIME
	szName          [248]uint16
}

// BLUETOOTH_AUTHENTICATION_CALLBACK_PARAMS — 576 B on x64. Leads with the
// 560-B device info, then three 4-B enums and a 4-B union (Numeric_Value /
// Passkey). Getting the leading deviceInfo offset right is what lets us read
// authenticationMethod at all.
type btAuthCallbackParams struct {
	deviceInfo                 btDeviceInfo // 560
	authenticationMethod       uint32       // BLUETOOTH_AUTHENTICATION_METHOD
	ioCapability               uint32       // BLUETOOTH_IO_CAPABILITY
	authenticationRequirements uint32       // BLUETOOTH_AUTHENTICATION_REQUIREMENTS
	numericValueOrPasskey      uint32       // union { ULONG Numeric_Value; ULONG Passkey }
}

// BLUETOOTH_AUTHENTICATION_RESPONSE — 48 B on x64. The response union is sized
// to its LARGEST arm, BLUETOOTH_OOB_DATA_INFO (C[16]+R[16] = 32 B) — NOT the
// numeric/passkey arm we actually fill. Under-sizing it would corrupt memory
// past the struct exactly like the devModeW incident.
type btAuthResponse struct {
	bthAddressRemote uint64   // BLUETOOTH_ADDRESS (8)
	authMethod       uint32   // BLUETOOTH_AUTHENTICATION_METHOD (4)
	oobUnion         [32]byte // union; the numeric/passkey value lives in the first 4 bytes
	negativeResponse uint8    // (1) + tail pad to 48
}

// BLUETOOTH_AUTHENTICATION_METHOD values.
const (
	authMethodLegacy              = 1 // PIN
	authMethodOOB                 = 2
	authMethodNumericComparison   = 3
	authMethodPasskeyNotification = 4
	authMethodPasskey             = 5
)

// BLUETOOTH_IO_CAPABILITY / BLUETOOTH_AUTHENTICATION_REQUIREMENTS.
const (
	ioCapNoInputNoOutput  = 0x0003 // a kiosk has no keypad → nudges Just Works
	authReqMITMNotRequired = 0x0000
)

// ─── Pairing state ────────────────────────────────────────────────────────

// pairing holds the addresses we're actively authenticating, so the global
// auth callback only auto-confirms devices the user explicitly chose to pair.
var (
	pairingMu sync.Mutex
	pairing   = map[uint64]struct{}{}

	// lastSeen caches the raw device info from the most recent inquiry, keyed
	// by our 12-hex MAC id, so Pair doesn't have to run a second 6-second
	// inquiry for a device the user just discovered and clicked.
	lastSeenMu sync.Mutex
	lastSeen   = map[string]btDeviceInfo{}

	authRegOnce   sync.Once
	authRegHandle uintptr
)

// authCallback is the PFN_AUTHENTICATION_CALLBACK_EX target. It runs on a
// Bluetooth-stack thread (NOT a Go goroutine), so it stays minimal: confirm
// the SSP request for a device we're pairing, then return. We correlate by
// address (never a Go pointer through pvParam).
func authCallback(_ uintptr, pParams uintptr) uintptr {
	if pParams == 0 {
		return 0
	}
	params := (*btAuthCallbackParams)(unsafe.Pointer(pParams))
	addr := params.deviceInfo.ullAddress

	pairingMu.Lock()
	_, active := pairing[addr]
	pairingMu.Unlock()
	if !active {
		return 0 // not ours — let Windows' default handling apply
	}

	var resp btAuthResponse
	resp.bthAddressRemote = addr
	resp.authMethod = params.authenticationMethod
	resp.negativeResponse = 0 // accept
	// Echo the numeric value for numeric-comparison / passkey methods into the
	// response union's first 4 bytes (little-endian on x86). Harmless for Just
	// Works. (binary.PutUint32 instead of an unsafe cast keeps go vet happy.)
	binary.LittleEndian.PutUint32(resp.oobUnion[0:4], params.numericValueOrPasskey)

	radio, closeRadio := firstRadio()
	defer closeRadio()
	procBluetoothSendAuthenticationResponseEx.Call(radio, uintptr(unsafe.Pointer(&resp)))
	return 1 // TRUE — handled
}

// authCallbackPtr is built ONCE — syscall.NewCallback trampolines are never
// freed and the process has a hard cap, so we must not create one per pair.
var authCallbackPtr = syscall.NewCallback(authCallback)

// ensureAuthRegistered registers our auth callback process-wide (NULL device =
// all devices) on first use. The registration handle lives for the process
// lifetime — pairing is rare and unregistering buys nothing.
func ensureAuthRegistered() {
	authRegOnce.Do(func() {
		if err := procBluetoothRegisterForAuthenticationEx.Find(); err != nil {
			slog.Warn("bluetooth: auth registration API unavailable", "err", err)
			return
		}
		procBluetoothRegisterForAuthenticationEx.Call(
			0, // pbtdiIn = NULL → all devices
			uintptr(unsafe.Pointer(&authRegHandle)),
			authCallbackPtr,
			0, // pvParam
		)
		if authRegHandle == 0 {
			slog.Warn("bluetooth: RegisterForAuthenticationEx returned no handle")
		}
	})
}

// ─── Public API ───────────────────────────────────────────────────────────

// Device is the JSON-friendly shape returned to the frontend.
type Device struct {
	ID         string `json:"id"`         // 12-hex MAC (no separators) — stable identifier
	Address    string `json:"address"`    // human MAC "AA:BB:CC:DD:EE:FF"
	Name       string `json:"name"`
	Connected  bool   `json:"connected"`  // radio/ACL link up (fConnected) — legacy alias of RadioConn
	Paired     bool   `json:"paired"`     // fAuthenticated — bonded with Windows
	Remembered bool   `json:"remembered"` // fRemembered — in the known-devices list
	IsAudio    bool   `json:"isAudio"`    // major class 0x04 = Audio/Video
	ClassHex   string `json:"classHex"`   // raw class-of-device, useful for debugging
}

// ConnectResult lets the UI distinguish "it worked" from the expected,
// non-fatal outcomes (device asleep, no audio profile, not paired yet) so it
// can show an actionable inline hint instead of a scary red error toast.
type ConnectResult struct {
	OK     bool   `json:"ok"`
	Status string `json:"status"` // connected | unreachable | no-audio-profile | not-paired | error
	Detail string `json:"detail"`
}

// runtimeMu serialises the slow blocking radio calls (inquiry ~6 s,
// SetServiceState up to ~30 s, pairing). Concurrent requests would just queue
// inside the radio anyway; serialising gives the frontend predictable timing.
var runtimeMu sync.Mutex

// List returns every PAIRED (remembered) bluetooth device — the fast, no-radio
// path used to render the picker's known devices immediately on open.
func List() ([]Device, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	raws, err := enumerate(false)
	if err != nil {
		return nil, err
	}
	out := make([]Device, 0, len(raws))
	for i := range raws {
		out = append(out, toDevice(&raws[i]))
	}
	return out, nil
}

// Discover runs ONE active classic-Bluetooth inquiry (~6.4 s), reporting each
// device via emit("bluetooth:found", Device) as it's found and returning the
// full de-duplicated list. Includes paired devices (so the UI can merge) and
// brand-new unpaired ones. Blocks — call from a goroutine.
func Discover(emit func(ev string, payload any)) ([]Device, error) {
	if emit == nil {
		emit = func(string, any) {}
	}
	runtimeMu.Lock()
	defer runtimeMu.Unlock()
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	raws, err := enumerate(true)
	if err != nil {
		return nil, err
	}
	out := make([]Device, 0, len(raws))
	lastSeenMu.Lock()
	for i := range raws {
		d := toDevice(&raws[i])
		lastSeen[d.ID] = raws[i]
		out = append(out, d)
		emit("bluetooth:found", d)
	}
	lastSeenMu.Unlock()
	return out, nil
}

// Pair authenticates an unpaired device by MAC id, then enables its audio
// services so it actually connects. Most modern audio gear uses SSP "Just
// Works" (no PIN), auto-confirmed by authCallback. Blocks — call from a
// goroutine. Emits bluetooth:pair-start / bluetooth:pair-done.
func Pair(deviceID string, emit func(ev string, payload any)) error {
	if emit == nil {
		emit = func(string, any) {}
	}
	if err := procBluetoothAuthenticateDeviceEx.Find(); err != nil {
		return fmt.Errorf("pairing API unavailable: %w", err)
	}
	ensureAuthRegistered()

	runtimeMu.Lock()
	defer runtimeMu.Unlock()
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	info, err := findDeviceForPairing(deviceID)
	if err != nil {
		emit("bluetooth:pair-done", map[string]any{"id": deviceID, "ok": false, "detail": err.Error()})
		return err
	}
	info.dwSize = uint32(unsafe.Sizeof(info))
	addr := info.ullAddress

	pairingMu.Lock()
	pairing[addr] = struct{}{}
	pairingMu.Unlock()
	defer func() {
		pairingMu.Lock()
		delete(pairing, addr)
		pairingMu.Unlock()
	}()

	emit("bluetooth:pair-start", map[string]any{"id": deviceID})

	radio, closeRadio := firstRadio()
	defer closeRadio()

	// BluetoothAuthenticateDeviceEx returns ERROR_SUCCESS (0) on success.
	r, _, callErr := procBluetoothAuthenticateDeviceEx.Call(
		0,                              // hwndParentIn = NULL → no OS dialog (works in shell mode)
		radio,                          // hRadioIn
		uintptr(unsafe.Pointer(&info)), // pbtdiInout
		0,                              // pbtOobData = NULL
		uintptr(authReqMITMNotRequired),
	)
	if r != errSuccess {
		detail := classifyPairErr(uint32(r))
		emit("bluetooth:pair-done", map[string]any{"id": deviceID, "ok": false, "detail": detail})
		return fmt.Errorf("pairing failed: %s (code %d): %v", detail, r, callErr)
	}

	// Bonded — enable audio so the headset actually connects/routes. We
	// already hold runtimeMu, so call the lock-free variant.
	_ = setServicesLocked(deviceID, true)
	emit("bluetooth:pair-done", map[string]any{"id": deviceID, "ok": true})
	return nil
}

// Connect best-effort enables ALL audio service profiles on a paired device.
// Kept for back-compat; new callers use ConnectEx for the richer result.
func Connect(deviceID string) error { return setServicesByID(deviceID, true) }

// Disconnect flips the audio services off.
func Disconnect(deviceID string) error { return setServicesByID(deviceID, false) }

// ConnectEx enables the audio services and reports a classified result instead
// of a bare error, so the UI can tell "asleep / no profile / not paired" apart
// from a genuine failure and avoid a scary toast. Idempotent — safe to press on
// an already-connected device that just isn't routing audio.
func ConnectEx(deviceID string) (ConnectResult, error) {
	runtimeMu.Lock()
	defer runtimeMu.Unlock()
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	raws, err := enumerate(false)
	if err != nil {
		return ConnectResult{Status: "error", Detail: err.Error()}, err
	}
	var target *btDeviceInfo
	for i := range raws {
		if macHex(raws[i].ullAddress) == deviceID {
			target = &raws[i]
			break
		}
	}
	if target == nil {
		// Not in the remembered list → the user hit Connect on a discovered,
		// still-unpaired device. Tell the UI to offer Pair instead.
		return ConnectResult{OK: false, Status: "not-paired", Detail: "device is not paired yet — pair it first"}, nil
	}

	radio, closeRadio := firstRadio()
	defer closeRadio()

	anyOK := false
	allNoProfile := true
	for i := range audioServiceGUIDs {
		g := audioServiceGUIDs[i]
		r, _, _ := procBluetoothSetServiceState.Call(
			radio,
			uintptr(unsafe.Pointer(target)),
			uintptr(unsafe.Pointer(&g)),
			btServiceEnable,
		)
		switch {
		case r == errSuccess:
			anyOK = true
			allNoProfile = false
		case r == errServiceDoesNotExist || r == errNotFound:
			// device simply doesn't expose this profile — clean skip
		case r == errDeviceNotConnected:
			return ConnectResult{OK: false, Status: "unreachable",
				Detail: "device not in range or powered off — turn it on / put it in pairing mode"}, nil
		default:
			allNoProfile = false
		}
	}
	if anyOK {
		return ConnectResult{OK: true, Status: "connected", Detail: "audio enabled"}, nil
	}
	if allNoProfile {
		return ConnectResult{OK: false, Status: "no-audio-profile", Detail: "device exposes no audio profile"}, nil
	}
	return ConnectResult{OK: false, Status: "unreachable", Detail: "device did not respond — turn it on and retry"}, nil
}

const (
	btServiceDisable = 0x00
	btServiceEnable  = 0x01
)

func setServicesByID(deviceID string, enable bool) error {
	runtimeMu.Lock()
	defer runtimeMu.Unlock()
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	return setServicesLocked(deviceID, enable)
}

// setServicesLocked is the body of the service flip. Caller MUST hold runtimeMu
// and have locked the OS thread (so Pair can reuse it without re-locking).
func setServicesLocked(deviceID string, enable bool) error {
	raws, err := enumerate(false)
	if err != nil {
		return err
	}
	var target *btDeviceInfo
	for i := range raws {
		if macHex(raws[i].ullAddress) == deviceID {
			target = &raws[i]
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

	// BluetoothSetServiceState needs a REAL radio handle — NULL is rejected on
	// Win10/11 with ERROR_INVALID_PARAMETER (87). And it reports its result in
	// the DWORD RETURN VALUE (r), not GetLastError (which is Errno(0) on this
	// no-SetLastError path — formatting it gave the v0.10.6 "errno 0: operation
	// completed successfully" nonsense).
	radio, closeRadio := firstRadio()
	defer closeRadio()

	var firstErr error
	anyOK := false
	for i := range audioServiceGUIDs {
		g := audioServiceGUIDs[i]
		r, _, _ := procBluetoothSetServiceState.Call(
			radio,
			uintptr(unsafe.Pointer(target)),
			uintptr(unsafe.Pointer(&g)),
			flag,
		)
		if r == errSuccess {
			anyOK = true
			continue
		}
		if r == errServiceDoesNotExist || r == errNotFound {
			continue // device doesn't expose this profile — clean skip
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

// ─── Enumeration ──────────────────────────────────────────────────────────

// enumerate runs a device find with either the remembered-only params or the
// active-inquiry params, returning the raw infos. The caller MUST have locked
// the OS thread (the HBLUETOOTH_DEVICE_FIND handle is thread-affine) and, for
// an inquiry, should hold runtimeMu (it drives the radio for seconds).
func enumerate(inquiry bool) ([]btDeviceInfo, error) {
	if err := procBluetoothFindFirstDevice.Find(); err != nil {
		return nil, fmt.Errorf("BluetoothApis.dll not available: %w", err)
	}
	params := btDeviceSearchParams{
		dwSize:               uint32(unsafe.Sizeof(btDeviceSearchParams{})),
		fReturnAuthenticated: 1,
		fReturnRemembered:    1,
		fReturnConnected:     1,
		cTimeoutMultiplier:   2,
	}
	if inquiry {
		// fReturnUnknown surfaces brand-new (unpaired) devices; fIssueInquiry
		// actively scans the air. Timeout = multiplier × 1.28 s → 5 ≈ 6.4 s.
		params.fReturnUnknown = 1
		params.fIssueInquiry = 1
		params.cTimeoutMultiplier = 5
	}
	var info btDeviceInfo
	info.dwSize = uint32(unsafe.Sizeof(info))

	hFind, _, err := procBluetoothFindFirstDevice.Call(
		uintptr(unsafe.Pointer(&params)),
		uintptr(unsafe.Pointer(&info)),
	)
	if hFind == 0 {
		if errno, ok := err.(syscall.Errno); ok && errno == errNoMoreItems {
			return nil, nil
		}
		return nil, fmt.Errorf("BluetoothFindFirstDevice: %w", err)
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

// findDeviceForPairing resolves a device id to a fresh btDeviceInfo for
// AuthenticateDeviceEx — preferring the cache from the last inquiry, falling
// back to a fresh inquiry. Caller (Pair) holds runtimeMu + the OS thread.
func findDeviceForPairing(deviceID string) (btDeviceInfo, error) {
	lastSeenMu.Lock()
	info, ok := lastSeen[deviceID]
	lastSeenMu.Unlock()
	if ok {
		return info, nil
	}
	raws, err := enumerate(true)
	if err != nil {
		return btDeviceInfo{}, err
	}
	for i := range raws {
		if macHex(raws[i].ullAddress) == deviceID {
			lastSeenMu.Lock()
			lastSeen[deviceID] = raws[i]
			lastSeenMu.Unlock()
			return raws[i], nil
		}
	}
	return btDeviceInfo{}, fmt.Errorf("device %s not found in range — put it in pairing mode", deviceID)
}

// ─── Helpers ──────────────────────────────────────────────────────────────

// firstRadio opens a handle to the first local Bluetooth radio so the radio
// APIs have a non-NULL hRadio to act on. The returned closeFn releases both
// handles. On failure returns (0, no-op) and the caller falls back to NULL.
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

func classifyPairErr(code uint32) string {
	switch code {
	case errDeviceNotConnected:
		return "device unreachable — turn it on and put it in pairing mode"
	case waitTimeout:
		return "timed out waiting for the device"
	case errNotAuthenticated:
		return "authentication rejected by the device"
	case errSessionCredConflict:
		return "conflicting pairing — remove the device in Windows and retry"
	case errInvalidParameter:
		return "the radio rejected the request"
	default:
		return fmt.Sprintf("error code %d", code)
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
		ID:         macHex(info.ullAddress),
		Address:    macColon(info.ullAddress),
		Name:       name,
		Connected:  info.fConnected != 0,
		Paired:     info.fAuthenticated != 0,
		Remembered: info.fRemembered != 0,
		IsAudio:    major == 0x04,
		ClassHex:   fmt.Sprintf("0x%06X", info.ulClassOfDevice&0xFFFFFF),
	}
}

// BLUETOOTH_ADDRESS.ullLong holds the 6-byte MAC in the low 48 bits,
// little-endian (byte 0 = LSB). Render big-endian "AA:BB:CC:DD:EE:FF".
func macColon(ull uint64) string {
	b := macBytes(ull)
	return fmt.Sprintf("%02X:%02X:%02X:%02X:%02X:%02X", b[0], b[1], b[2], b[3], b[4], b[5])
}

// macHex returns the same MAC without separators — the stable ID we hand to
// the frontend.
func macHex(ull uint64) string {
	b := macBytes(ull)
	return fmt.Sprintf("%02X%02X%02X%02X%02X%02X", b[0], b[1], b[2], b[3], b[4], b[5])
}

func macBytes(ull uint64) [6]byte {
	return [6]byte{
		byte(ull >> 40), byte(ull >> 32), byte(ull >> 24),
		byte(ull >> 16), byte(ull >> 8), byte(ull),
	}
}

// unregisterAuth is currently unused (we keep the process-wide registration for
// the app's lifetime) but kept wired so a future explicit teardown is one call.
func unregisterAuth() {
	if authRegHandle != 0 {
		procBluetoothUnregisterAuthentication.Call(authRegHandle)
		authRegHandle = 0
	}
}
