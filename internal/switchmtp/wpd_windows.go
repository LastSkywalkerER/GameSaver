//go:build windows

// Package switchmtp talks to an MTP device (specifically a Nintendo Switch
// running the DBI MTP responder) through the Windows Portable Devices (WPD)
// COM API.
//
// Why WPD and not something else:
//   - libmtp / go-mtpfs need libusb, which on Windows means swapping the
//     device's driver with Zadig. That breaks MTP in Explorer for the user —
//     unacceptable for a desktop utility.
//   - Shell.Application's CopyHere works for reads but is asynchronous and
//     reports no errors at all, so a failed transfer is indistinguishable
//     from a slow one.
//
// None of the WPD interfaces are exposed by x/sys/windows, so every call here
// goes through a raw vtable index. The GUIDs below were read out of
// HKLM\SOFTWARE\Classes\{CLSID,Interface} on a live machine rather than typed
// from memory — IPortableDeviceValues in particular is easy to get wrong, and
// a wrong IID fails silently as a QueryInterface miss rather than a crash.
package switchmtp

import (
	"fmt"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ─── CLSIDs / IIDs (verified against the registry) ─────────────────────────

var (
	clsidPortableDeviceManager = guid("{0AF10CEC-2ECD-4B92-9581-34F6AE0637F3}")
	clsidPortableDeviceFTM     = guid("{F7C0039A-4762-488A-B4B3-760EF9A1BA9B}")
	clsidPortableDeviceValues  = guid("{0C15D503-D017-47CE-9016-7B3F978721CC}")

	iidPortableDeviceManager = guid("{A1567595-4C2F-4574-A6FA-ECEF917B9A40}")
	iidPortableDevice        = guid("{625E2DF8-6392-4CF0-9AD1-3CFA5F17775C}")
	iidPortableDeviceValues  = guid("{6848F6F2-3155-4F86-B6F5-263EEEAB3143}")
)

// ─── PROPERTYKEYs (not registry-discoverable; verified empirically) ─────────

// propertyKey mirrors the Win32 PROPERTYKEY: a GUID plus a DWORD. GUID is
// 4-byte aligned so this is exactly 20 bytes with no padding — do not add any.
type propertyKey struct {
	fmtid windows.GUID
	pid   uint32
}

var (
	fmtidObjectProps = guid("{EF6B490D-5CD8-437A-AFFC-DA8B60EE4A3C}")
	fmtidClientInfo  = guid("{204D9F0C-2292-4080-9F42-40664E70F859}")
	fmtidResourceDef = guid("{E81E79BE-34F0-41BF-B53F-F1A06AE87842}")

	keyObjectParentID = propertyKey{fmtidObjectProps, 3}
	keyObjectName     = propertyKey{fmtidObjectProps, 4}
	keyObjectFormat   = propertyKey{fmtidObjectProps, 6}
	keyObjectContent  = propertyKey{fmtidObjectProps, 7}
	keyObjectSize     = propertyKey{fmtidObjectProps, 11}
	keyObjectOrigName = propertyKey{fmtidObjectProps, 12}

	keyClientName     = propertyKey{fmtidClientInfo, 2}
	keyClientMajorVer = propertyKey{fmtidClientInfo, 3}
	keyClientMinorVer = propertyKey{fmtidClientInfo, 4}
	keyClientRevision = propertyKey{fmtidClientInfo, 5}
	// WPD_CLIENT_DESIRED_ACCESS. Passing GENERIC_READ here is our hard
	// guarantee that a scan/backup session physically cannot modify the
	// console: the driver opens the device read-only, so even a bug in the
	// upload path cannot write through a read session.
	keyClientDesiredAccess = propertyKey{fmtidClientInfo, 9}

	keyResourceDefault = propertyKey{fmtidResourceDef, 0}

	contentTypeFolder     = guid("{27E2E392-A111-48E0-AB0C-E17705A05F85}")
	contentTypeFunctional = guid("{99ED0160-17FF-4C44-9D98-1D7A6F941921}")
	contentTypeGenericFile = guid("{0085E0A6-8D34-45D7-BC5C-447E59C73D48}")
	objectFormatUnspecified = guid("{30000000-AE6C-4804-98BA-C57B46965FE7}")
)

const (
	genericRead  = 0x80000000
	genericWrite = 0x40000000

	// WPD_DEVICE_OBJECT_ID — the well-known root object every device exposes.
	deviceObjectID = "DEVICE"

	streamRead  = 0 // STGM_READ
	streamWrite = 1 // STGM_WRITE
)

// ─── raw COM plumbing ──────────────────────────────────────────────────────

func guid(s string) windows.GUID {
	g, err := windows.GUIDFromString(s)
	if err != nil {
		panic("switchmtp: bad GUID literal " + s)
	}
	return g
}

// x/sys/windows ships CoInitializeEx/CoUninitialize/CoTaskMemFree but not
// CoCreateInstance, so we bind it ourselves rather than pull go-ole into the
// direct dependency set for one function.
var (
	modOle32             = windows.NewLazySystemDLL("ole32.dll")
	procCoCreateInstance = modOle32.NewProc("CoCreateInstance")
)

const clsctxInprocServer = 0x1

func coCreateInstance(clsid, iid *windows.GUID, out *uintptr) error {
	if err := procCoCreateInstance.Find(); err != nil {
		return err
	}
	r, _, _ := syscall.SyscallN(procCoCreateInstance.Addr(),
		uintptr(unsafe.Pointer(clsid)),
		0, // pUnkOuter — no aggregation
		uintptr(clsctxInprocServer),
		uintptr(unsafe.Pointer(iid)),
		uintptr(unsafe.Pointer(out)))
	return hr(r, "CoCreateInstance")
}

// call invokes vtable slot idx on a COM interface pointer. Slots 0..2 are
// IUnknown (QueryInterface/AddRef/Release), so interface-specific methods
// start at 3.
//
// 🔴 The //go:uintptrescapes directive is load-bearing, not decoration. Callers
// pass Go pointers as uintptr(unsafe.Pointer(&x)); without this directive the
// compiler is free to consider x dead the moment the conversion happens, and
// the allocation inside this function can trigger a GC or a stack growth
// before the syscall runs — handing the driver a dangling address. The
// directive forces those arguments to be heap-allocated and kept alive for
// the duration of the call.
//
//go:uintptrescapes
func call(this uintptr, idx uintptr, args ...uintptr) uintptr {
	// `go vet` reports "possible misuse of unsafe.Pointer" on the next two
	// lines. It is a false positive and must not be "fixed": `this` points at
	// a COM object allocated by PortableDeviceApi.dll on the native heap, not
	// at Go-managed memory, so reading its vtable through a uintptr is exactly
	// the intended pattern. (internal/audio and internal/bluetooth carry the
	// same warning for the same reason.)
	vtbl := *(*uintptr)(unsafe.Pointer(this))
	fn := *(*uintptr)(unsafe.Pointer(vtbl + idx*unsafe.Sizeof(uintptr(0))))
	full := make([]uintptr, 0, len(args)+1)
	full = append(full, this)
	full = append(full, args...)
	r, _, _ := syscall.SyscallN(fn, full...)
	return r
}

func release(this uintptr) {
	if this != 0 {
		call(this, 2)
	}
}

// hr converts an HRESULT into an error. COM signals failure with the sign bit,
// so the comparison must be on the signed value, not the raw uintptr.
func hr(code uintptr, what string) error {
	if int32(code) < 0 {
		return fmt.Errorf("%s: HRESULT 0x%08X", what, uint32(code))
	}
	return nil
}

func utf16(s string) *uint16 {
	p, err := windows.UTF16PtrFromString(s)
	if err != nil {
		return nil
	}
	return p
}

// takeString converts a CoTaskMemAlloc'd LPWSTR returned by WPD into a Go
// string and frees it. Every string-out parameter in this API is owned by the
// caller, so skipping the free leaks per object enumerated.
func takeString(p *uint16) string {
	if p == nil {
		return ""
	}
	s := windows.UTF16PtrToString(p)
	windows.CoTaskMemFree(unsafe.Pointer(p))
	return s
}

// ─── IPortableDeviceValues ─────────────────────────────────────────────────

type deviceValues uintptr

func newDeviceValues() (deviceValues, error) {
	var p uintptr
	if err := coCreateInstance(&clsidPortableDeviceValues, &iidPortableDeviceValues, &p); err != nil {
		return 0, fmt.Errorf("create PortableDeviceValues: %w", err)
	}
	return deviceValues(p), nil
}

func (v deviceValues) release() { release(uintptr(v)) }

// Vtable indices below follow the IPortableDeviceValues IDL method order after
// IUnknown's three slots: GetCount(3) GetAt(4) SetValue(5) GetValue(6)
// SetStringValue(7) GetStringValue(8) SetUnsignedIntegerValue(9) …
// SetUnsignedLargeIntegerValue(13) GetUnsignedLargeIntegerValue(14) …
// SetGuidValue(27) GetGuidValue(28). An off-by-one here calls a neighbouring
// method with mismatched arguments, which corrupts the stack rather than
// returning a clean error — count them, don't guess.
func (v deviceValues) setString(k propertyKey, val string) error {
	return hr(call(uintptr(v), 7, uintptr(unsafe.Pointer(&k)), uintptr(unsafe.Pointer(utf16(val)))), "SetStringValue")
}

func (v deviceValues) getString(k propertyKey) (string, error) {
	var out *uint16
	if err := hr(call(uintptr(v), 8, uintptr(unsafe.Pointer(&k)), uintptr(unsafe.Pointer(&out))), "GetStringValue"); err != nil {
		return "", err
	}
	return takeString(out), nil
}

func (v deviceValues) setUint32(k propertyKey, val uint32) error {
	return hr(call(uintptr(v), 9, uintptr(unsafe.Pointer(&k)), uintptr(val)), "SetUnsignedIntegerValue")
}

func (v deviceValues) getUint64(k propertyKey) (uint64, error) {
	var out uint64
	if err := hr(call(uintptr(v), 14, uintptr(unsafe.Pointer(&k)), uintptr(unsafe.Pointer(&out))), "GetUnsignedLargeIntegerValue"); err != nil {
		return 0, err
	}
	return out, nil
}

func (v deviceValues) setUint64(k propertyKey, val uint64) error {
	return hr(call(uintptr(v), 13, uintptr(unsafe.Pointer(&k)), uintptr(val)), "SetUnsignedLargeIntegerValue")
}

func (v deviceValues) getGUID(k propertyKey) (windows.GUID, error) {
	var out windows.GUID
	if err := hr(call(uintptr(v), 28, uintptr(unsafe.Pointer(&k)), uintptr(unsafe.Pointer(&out))), "GetGuidValue"); err != nil {
		return windows.GUID{}, err
	}
	return out, nil
}

func (v deviceValues) setGUID(k propertyKey, val windows.GUID) error {
	return hr(call(uintptr(v), 27, uintptr(unsafe.Pointer(&k)), uintptr(unsafe.Pointer(&val))), "SetGuidValue")
}

// ─── IPortableDeviceKeyCollection ──────────────────────────────────────────

type keyCollection uintptr

var (
	clsidPortableDeviceKeyCollection = guid("{DE2D022D-2480-43BE-97F0-D1FA2CF98F4F}")
	iidPortableDeviceKeyCollection   = guid("{DADA2357-E0AD-492E-98DB-DD61C53BA353}")
)

func newKeyCollection(keys ...propertyKey) (keyCollection, error) {
	var p uintptr
	if err := coCreateInstance(&clsidPortableDeviceKeyCollection, &iidPortableDeviceKeyCollection, &p); err != nil {
		return 0, fmt.Errorf("create PortableDeviceKeyCollection: %w", err)
	}
	kc := keyCollection(p)
	for _, k := range keys {
		if err := hr(call(p, 5, uintptr(unsafe.Pointer(&k))), "KeyCollection.Add"); err != nil {
			kc.release()
			return 0, err
		}
	}
	return kc, nil
}

func (k keyCollection) release() { release(uintptr(k)) }

// ─── IStream ───────────────────────────────────────────────────────────────

// comStream adapts an IStream to io.Reader / io.Writer. Vtable layout is
// IUnknown(0-2) + ISequentialStream Read(3)/Write(4) + IStream Seek(5) …
// Commit(8) … so the indices below are not arbitrary.
type comStream uintptr

func (s comStream) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	var read uint32
	r := call(uintptr(s), 3, uintptr(unsafe.Pointer(&p[0])), uintptr(uint32(len(p))), uintptr(unsafe.Pointer(&read)))
	if err := hr(r, "IStream.Read"); err != nil {
		return 0, err
	}
	return int(read), nil
}

func (s comStream) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	var wrote uint32
	r := call(uintptr(s), 4, uintptr(unsafe.Pointer(&p[0])), uintptr(uint32(len(p))), uintptr(unsafe.Pointer(&wrote)))
	if err := hr(r, "IStream.Write"); err != nil {
		return 0, err
	}
	return int(wrote), nil
}

// commit flushes an upload. MTP writes are not durable until this returns —
// dropping the stream without committing leaves a truncated object.
func (s comStream) commit() error {
	return hr(call(uintptr(s), 8, 0), "IStream.Commit")
}

func (s comStream) release() { release(uintptr(s)) }
