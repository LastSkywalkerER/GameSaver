// Volume control for the system default render endpoint, via
// IAudioEndpointVolume (the COM interface the Windows volume tray icon
// drives). Exposed as Get / StepUp / StepDown / Mute / IsMuted for the
// gamepad-driven volume modal in the shell UI.
//
// Why step-based and not absolute-set: the canonical
// SetMasterVolumeLevelScalar takes a `float` argument, and Windows x64
// passes floats in XMM1 (not RCX), which Go's syscall.SyscallN can't
// target without inline assembly or purego. The Step variants take only
// `this + context*` (both pointer-sized) so they work cleanly with the
// existing callVTable helper. Each StepUp is a ~2% bump (driver-defined,
// usually 1/50 of the dB range); for the picker we coalesce multiple
// steps into one "set to X%" by computing the delta in steps and looping.

package audio

import (
	"errors"
	"fmt"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

// IAudioEndpointVolume IID. Activated off an IMMDevice via Activate(),
// no CoCreateInstance — the endpoint's own implementation backs it.
var iidIAudioEndpointVolume = windows.GUID{
	Data1: 0x5CDF2C82, Data2: 0x841E, Data3: 0x4546,
	Data4: [8]byte{0x97, 0x22, 0x0C, 0xF7, 0x40, 0x78, 0x22, 0x9A},
}

// Vtable slots for IAudioEndpointVolume. Slot 0..2 are IUnknown.
// IAudioEndpointVolume layout (header order, byte-exact with the
// public endpointvolume.h):
//   3  RegisterControlChangeNotify
//   4  UnregisterControlChangeNotify
//   5  GetChannelCount
//   6  SetMasterVolumeLevel       (float dB)  — needs XMM register
//   7  SetMasterVolumeLevelScalar (float 0..1) — needs XMM register
//   8  GetMasterVolumeLevel       (out float dB)
//   9  GetMasterVolumeLevelScalar (out float 0..1)
//   10 SetChannelVolumeLevel
//   11 SetChannelVolumeLevelScalar
//   12 GetChannelVolumeLevel
//   13 GetChannelVolumeLevelScalar
//   14 SetMute(BOOL, ctx*)
//   15 GetMute(out BOOL)
//   16 GetVolumeStepInfo(out step, out stepCount)
//   17 VolumeStepUp(ctx*)
//   18 VolumeStepDown(ctx*)
//   19 QueryHardwareSupport
//   20 GetVolumeRange
const (
	vtVolGetScalar     = 9
	vtVolSetMute       = 14
	vtVolGetMute       = 15
	vtVolStepInfo      = 16
	vtVolStepUp        = 17
	vtVolStepDown      = 18
)

// VolumeInfo is the read-only snapshot returned to the UI.
type VolumeInfo struct {
	Level     float32 `json:"level"`     // 0.0..1.0, the scalar volume
	Muted     bool    `json:"muted"`
	Step      uint32  `json:"step"`      // current step index (0..StepCount)
	StepCount uint32  `json:"stepCount"` // total step count from the driver
}

// openDefaultEndpointVolume locks the OS thread, initialises COM,
// resolves the IAudioEndpointVolume of the default render endpoint,
// and runs `fn` against it. On return everything is torn down.
//
// Pattern is split from the IAudioEndpointVolume lookup because each
// public call (GetVolume, StepUp, StepDown, Mute, IsMuted) is short and
// independent — keeping the resource the for-duration-of-fn shape lets
// the caller stay tiny.
func openDefaultEndpointVolume(fn func(epv uintptr) error) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	r, _, _ := procCoInitializeEx.Call(0, coinitMultiThreaded)
	if int32(r) < 0 {
		return fmt.Errorf("CoInitializeEx hr=0x%x", r)
	}
	defer procCoUninitialize.Call()

	enum, err := coCreateInstance(&clsidMMDeviceEnumerator, &iidIMMDeviceEnumerator)
	if err != nil {
		return err
	}
	defer release(enum)

	var dev uintptr
	hr := callVTable(enum, vtGetDefaultAudioEndpoint,
		uintptr(eRender), uintptr(eMultimedia),
		uintptr(unsafe.Pointer(&dev)))
	if int32(hr) < 0 || dev == 0 {
		return fmt.Errorf("GetDefaultAudioEndpoint hr=0x%x", hr)
	}
	defer release(dev)

	// IMMDevice::Activate(iid, clsctx, params, out**) is the canonical
	// way to get an interface implemented by the endpoint itself.
	// Vtable slot 3 on IMMDevice.
	const vtMMDeviceActivate = 3
	var epv uintptr
	hr = callVTable(dev, vtMMDeviceActivate,
		uintptr(unsafe.Pointer(&iidIAudioEndpointVolume)),
		uintptr(clsctxAll), 0,
		uintptr(unsafe.Pointer(&epv)))
	if int32(hr) < 0 || epv == 0 {
		return fmt.Errorf("MMDevice.Activate(IAudioEndpointVolume) hr=0x%x", hr)
	}
	defer release(epv)

	return fn(epv)
}

// GetVolume snapshots the current default render endpoint's level,
// mute state, and step counters. The shell volume modal calls it on
// open + after each step to refresh its visual.
func GetVolume() (*VolumeInfo, error) {
	out := &VolumeInfo{}
	err := openDefaultEndpointVolume(func(epv uintptr) error {
		var level float32
		hr := callVTable(epv, vtVolGetScalar, uintptr(unsafe.Pointer(&level)))
		if int32(hr) < 0 {
			return fmt.Errorf("GetMasterVolumeLevelScalar hr=0x%x", hr)
		}
		out.Level = level

		var muted int32
		hr = callVTable(epv, vtVolGetMute, uintptr(unsafe.Pointer(&muted)))
		if int32(hr) < 0 {
			return fmt.Errorf("GetMute hr=0x%x", hr)
		}
		out.Muted = muted != 0

		var step, stepCount uint32
		hr = callVTable(epv, vtVolStepInfo,
			uintptr(unsafe.Pointer(&step)),
			uintptr(unsafe.Pointer(&stepCount)))
		if int32(hr) < 0 {
			return fmt.Errorf("GetVolumeStepInfo hr=0x%x", hr)
		}
		out.Step, out.StepCount = step, stepCount
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// StepUp bumps the volume by n steps (clamped to whatever the driver's
// step count is). One step is whatever Windows defines for this endpoint
// — typically 2% of the dB range. UI binds ←/→ to ±1, LB/RB to ±5.
func StepUp(n int) error {
	if n <= 0 {
		return nil
	}
	return openDefaultEndpointVolume(func(epv uintptr) error {
		for i := 0; i < n; i++ {
			hr := callVTable(epv, vtVolStepUp, 0)
			if int32(hr) < 0 {
				return fmt.Errorf("VolumeStepUp hr=0x%x", hr)
			}
		}
		return nil
	})
}

// StepDown is the mirror of StepUp.
func StepDown(n int) error {
	if n <= 0 {
		return nil
	}
	return openDefaultEndpointVolume(func(epv uintptr) error {
		for i := 0; i < n; i++ {
			hr := callVTable(epv, vtVolStepDown, 0)
			if int32(hr) < 0 {
				return fmt.Errorf("VolumeStepDown hr=0x%x", hr)
			}
		}
		return nil
	})
}

// SetMute writes the BOOL mute flag on the endpoint. mute=true silences
// without touching the level — when the user toggles back the previous
// volume is preserved. Same semantics as the speaker icon in the system
// tray.
func SetMute(mute bool) error {
	flag := uintptr(0)
	if mute {
		flag = 1
	}
	return openDefaultEndpointVolume(func(epv uintptr) error {
		hr := callVTable(epv, vtVolSetMute, flag, 0)
		if int32(hr) < 0 {
			return fmt.Errorf("SetMute hr=0x%x", hr)
		}
		return nil
	})
}

// IsMuted is a thin convenience over GetVolume for tray-icon-like
// callers that only care about the boolean.
func IsMuted() (bool, error) {
	v, err := GetVolume()
	if err != nil {
		return false, err
	}
	return v.Muted, nil
}

// internal sanity helper — keeps the "I forgot to wire something" tax
// at compile time instead of runtime.
var _ = errors.New
