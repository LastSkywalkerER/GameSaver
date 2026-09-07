//go:build windows

package switchmtp

import (
	"fmt"
	"io"
	"runtime"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Device is one MTP device as reported by the WPD device manager.
type Device struct {
	PnPID        string `json:"pnpId"`
	FriendlyName string `json:"friendlyName"`
	Description  string `json:"description"`
	Manufacturer string `json:"manufacturer"`
	Serial       string `json:"serial"`
}

// switchVendorID is Nintendo's USB vendor ID. The DBI MTP responder enumerates
// as VID_057E&PID_201D, but we match on the vendor alone so a different
// responder build (or a future PID) is still recognised as a Switch.
const switchVendorID = "VID_057E"

// IsSwitch reports whether this device is a Nintendo console.
func (d Device) IsSwitch() bool {
	return strings.Contains(strings.ToUpper(d.PnPID), switchVendorID)
}

// serialFromPnPID pulls the device serial out of a WPD PnP id, which looks
// like `\\?\usb#vid_057e&pid_201d#xtj10668389812#{guid}` — the third
// '#'-separated field. Used to key backups to a specific console so two
// Switches don't get merged into one backup history.
func serialFromPnPID(id string) string {
	parts := strings.Split(id, "#")
	if len(parts) >= 3 {
		return parts[2]
	}
	return ""
}

// withCOM runs fn on a dedicated, OS-locked thread with COM initialised.
// COM apartments are per-thread state, and Go freely migrates goroutines
// between threads, so every COM interface pointer we create must be created
// and released on this one thread.
func withCOM(fn func() error) error {
	errc := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		// MTA: WPD is happy in the multithreaded apartment, and it spares us
		// from having to pump a message loop on this thread.
		//
		// A non-nil error here is almost always RPC_E_CHANGED_MODE, meaning
		// the thread already belongs to an STA. WPD still works in that case;
		// we just must not CoUninitialize an apartment we didn't create.
		ownsApartment := windows.CoInitializeEx(0, windows.COINIT_MULTITHREADED) == nil
		if ownsApartment {
			defer windows.CoUninitialize()
		}
		errc <- fn()
	}()
	return <-errc
}

// ListDevices enumerates every portable device currently attached.
func ListDevices() ([]Device, error) {
	var out []Device
	err := withCOM(func() error {
		var mgr uintptr
		if err := coCreateInstance(&clsidPortableDeviceManager, &iidPortableDeviceManager, &mgr); err != nil {
			return fmt.Errorf("create PortableDeviceManager: %w", err)
		}
		defer release(mgr)

		// Two-call pattern: ask for the count first, then fill the array.
		var count uint32
		if err := hr(call(mgr, 3, 0, uintptr(unsafe.Pointer(&count))), "GetDevices(count)"); err != nil {
			return err
		}
		if count == 0 {
			return nil
		}
		ids := make([]*uint16, count)
		if err := hr(call(mgr, 3, uintptr(unsafe.Pointer(&ids[0])), uintptr(unsafe.Pointer(&count))), "GetDevices"); err != nil {
			return err
		}
		for i := uint32(0); i < count; i++ {
			pnp := takeString(ids[i])
			if pnp == "" {
				continue
			}
			d := Device{PnPID: pnp, Serial: serialFromPnPID(pnp)}
			d.FriendlyName = deviceStringProp(mgr, 5, pnp)
			d.Description = deviceStringProp(mgr, 6, pnp)
			d.Manufacturer = deviceStringProp(mgr, 7, pnp)
			out = append(out, d)
		}
		return nil
	})
	return out, err
}

// deviceStringProp calls one of the IPortableDeviceManager
// GetDeviceFriendlyName/Description/Manufacturer trio, all of which share the
// same (id, buffer, *cch) two-call shape. A device that doesn't publish the
// property just yields "" rather than failing the whole enumeration.
func deviceStringProp(mgr uintptr, idx uintptr, pnpID string) string {
	id := utf16(pnpID)
	var cch uint32
	if err := hr(call(mgr, idx, uintptr(unsafe.Pointer(id)), 0, uintptr(unsafe.Pointer(&cch))), "len"); err != nil || cch == 0 {
		return ""
	}
	buf := make([]uint16, cch)
	if err := hr(call(mgr, idx, uintptr(unsafe.Pointer(id)), uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&cch))), "get"); err != nil {
		return ""
	}
	return windows.UTF16ToString(buf)
}

// FindSwitch returns the first attached Nintendo console, if any.
func FindSwitch() (Device, bool, error) {
	devs, err := ListDevices()
	if err != nil {
		return Device{}, false, err
	}
	for _, d := range devs {
		if d.IsSwitch() {
			return d, true, nil
		}
	}
	return Device{}, false, nil
}

// ─── sessions ──────────────────────────────────────────────────────────────

// Session is an open connection to one device. It is only valid inside the
// WithSession callback that created it, because all of its COM pointers belong
// to that callback's OS thread.
type Session struct {
	device   uintptr // IPortableDevice
	content  uintptr // IPortableDeviceContent
	props    uintptr // IPortableDeviceProperties
	res      uintptr // IPortableDeviceResources
	writable bool
}

// Object is one node in the device's content tree.
type Object struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Size  int64  `json:"size"`
	IsDir bool   `json:"isDir"`
}

// WithSession opens the device, runs fn, and tears everything down again.
//
// writable=false requests GENERIC_READ from the driver. That is not a
// courtesy flag: a read-only session physically cannot modify the console, so
// scanning and backup cannot damage save data even if the code above them is
// wrong. Only the explicit PC→Switch transfer path passes writable=true.
func WithSession(pnpID string, writable bool, fn func(*Session) error) error {
	return withCOM(func() error {
		s, err := openSession(pnpID, writable)
		if err != nil {
			return err
		}
		defer s.close()
		return fn(s)
	})
}

func openSession(pnpID string, writable bool) (*Session, error) {
	info, err := newDeviceValues()
	if err != nil {
		return nil, err
	}
	defer info.release()
	_ = info.setString(keyClientName, "GameSaver")
	_ = info.setUint32(keyClientMajorVer, 1)
	_ = info.setUint32(keyClientMinorVer, 0)
	_ = info.setUint32(keyClientRevision, 0)
	access := uint32(genericRead)
	if writable {
		access = genericRead | genericWrite
	}
	_ = info.setUint32(keyClientDesiredAccess, access)

	var dev uintptr
	// The FTM ("free-threaded marshaler") variant is the one to use from a
	// multithreaded apartment; the plain CLSID_PortableDevice would force
	// every call through a proxy.
	if err := coCreateInstance(&clsidPortableDeviceFTM, &iidPortableDevice, &dev); err != nil {
		return nil, fmt.Errorf("create PortableDevice: %w", err)
	}
	s := &Session{device: dev, writable: writable}
	if err := hr(call(dev, 3, uintptr(unsafe.Pointer(utf16(pnpID))), uintptr(info)), "IPortableDevice.Open"); err != nil {
		s.close()
		return nil, err
	}
	if err := hr(call(dev, 5, uintptr(unsafe.Pointer(&s.content))), "IPortableDevice.Content"); err != nil {
		s.close()
		return nil, err
	}
	if err := hr(call(s.content, 4, uintptr(unsafe.Pointer(&s.props))), "Content.Properties"); err != nil {
		s.close()
		return nil, err
	}
	if err := hr(call(s.content, 5, uintptr(unsafe.Pointer(&s.res))), "Content.Transfer"); err != nil {
		s.close()
		return nil, err
	}
	return s, nil
}

func (s *Session) close() {
	release(s.res)
	release(s.props)
	release(s.content)
	if s.device != 0 {
		call(s.device, 8) // IPortableDevice.Close
		release(s.device)
	}
}

// RootID is the object id of the device root; children of it are the storages
// ("1: SD Card", "7: Saves", …).
func (s *Session) RootID() string { return deviceObjectID }

// Children lists the direct children of an object.
func (s *Session) Children(parentID string) ([]Object, error) {
	var enum uintptr
	if err := hr(call(s.content, 3, 0, uintptr(unsafe.Pointer(utf16(parentID))), 0, uintptr(unsafe.Pointer(&enum))),
		"Content.EnumObjects"); err != nil {
		return nil, err
	}
	defer release(enum)

	// One key collection reused for every child — building it per object turns
	// a 700-object walk into 700 COM object creations.
	keys, err := newKeyCollection(keyObjectName, keyObjectOrigName, keyObjectSize, keyObjectContent)
	if err != nil {
		return nil, err
	}
	defer keys.release()

	out := []Object{}
	const batch = 32
	ids := make([]*uint16, batch)
	for {
		var fetched uint32
		r := call(enum, 3, uintptr(batch), uintptr(unsafe.Pointer(&ids[0])), uintptr(unsafe.Pointer(&fetched)))
		if err := hr(r, "Enum.Next"); err != nil {
			return nil, err
		}
		if fetched == 0 {
			break
		}
		for i := uint32(0); i < fetched; i++ {
			id := takeString(ids[i])
			if id == "" {
				continue
			}
			obj, err := s.describe(id, keys)
			if err != nil {
				// A single unreadable object shouldn't sink the listing —
				// DBI exposes a few pseudo-entries without full properties.
				continue
			}
			out = append(out, obj)
		}
		if fetched < batch {
			break
		}
	}
	return out, nil
}

// describe reads the properties we care about for one object.
func (s *Session) describe(id string, keys keyCollection) (Object, error) {
	var vals uintptr
	if err := hr(call(s.props, 5, uintptr(unsafe.Pointer(utf16(id))), uintptr(keys), uintptr(unsafe.Pointer(&vals))),
		"Properties.GetValues"); err != nil {
		return Object{}, err
	}
	v := deviceValues(vals)
	defer v.release()

	obj := Object{ID: id}
	// Files carry a real filename in ORIGINAL_FILE_NAME; storages and some
	// folders only have the display NAME. Prefer the former, fall back.
	if n, err := v.getString(keyObjectOrigName); err == nil && n != "" {
		obj.Name = n
	} else if n, err := v.getString(keyObjectName); err == nil {
		obj.Name = n
	}
	if sz, err := v.getUint64(keyObjectSize); err == nil {
		obj.Size = int64(sz)
	}
	if ct, err := v.getGUID(keyObjectContent); err == nil {
		obj.IsDir = ct == contentTypeFolder || ct == contentTypeFunctional
	}
	if obj.Name == "" {
		return Object{}, fmt.Errorf("object %s has no name", id)
	}
	return obj, nil
}

// Child finds a direct child by name (case-insensitive).
func (s *Session) Child(parentID, name string) (Object, bool, error) {
	kids, err := s.Children(parentID)
	if err != nil {
		return Object{}, false, err
	}
	for _, k := range kids {
		if strings.EqualFold(k.Name, name) {
			return k, true, nil
		}
	}
	return Object{}, false, nil
}

// Resolve walks a slash-free path segment list down from the device root.
func (s *Session) Resolve(parts ...string) (Object, error) {
	cur := Object{ID: deviceObjectID, IsDir: true, Name: "/"}
	for _, p := range parts {
		next, ok, err := s.Child(cur.ID, p)
		if err != nil {
			return Object{}, err
		}
		if !ok {
			return Object{}, fmt.Errorf("не найдено на устройстве: %s", strings.Join(parts, "/"))
		}
		cur = next
	}
	return cur, nil
}

// WalkEntry is one file found by Walk, with its path relative to the walk root.
type WalkEntry struct {
	Rel  string
	ID   string
	Size int64
}

// Walk collects every file under root, depth-first. Directories themselves are
// not emitted — empty ones carry no save data and MTP has no permissions to
// preserve.
func (s *Session) Walk(rootID string) ([]WalkEntry, error) {
	var out []WalkEntry
	var rec func(id, prefix string, depth int) error
	rec = func(id, prefix string, depth int) error {
		// Guard against a pathological/looping tree; real save trees are
		// nowhere near this deep.
		if depth > 24 {
			return nil
		}
		kids, err := s.Children(id)
		if err != nil {
			return err
		}
		for _, k := range kids {
			rel := k.Name
			if prefix != "" {
				rel = prefix + "/" + k.Name
			}
			if k.IsDir {
				if err := rec(k.ID, rel, depth+1); err != nil {
					return err
				}
				continue
			}
			out = append(out, WalkEntry{Rel: rel, ID: k.ID, Size: k.Size})
		}
		return nil
	}
	if err := rec(rootID, "", 0); err != nil {
		return nil, err
	}
	return out, nil
}

// Open returns a reader over an object's default resource (its file contents).
func (s *Session) Open(objectID string) (io.ReadCloser, error) {
	var stream uintptr
	var optimal uint32
	k := keyResourceDefault
	if err := hr(call(s.res, 5,
		uintptr(unsafe.Pointer(utf16(objectID))),
		uintptr(unsafe.Pointer(&k)),
		uintptr(streamRead),
		uintptr(unsafe.Pointer(&optimal)),
		uintptr(unsafe.Pointer(&stream))), "Resources.GetStream"); err != nil {
		return nil, err
	}
	return &objectReader{s: comStream(stream)}, nil
}

// objectReader adapts IStream to io.ReadCloser. IStream signals end-of-data by
// returning zero bytes with S_OK, which io.Reader callers would spin on
// forever — translate it to io.EOF.
type objectReader struct{ s comStream }

func (r *objectReader) Read(p []byte) (int, error) {
	n, err := r.s.Read(p)
	if err != nil {
		return 0, err
	}
	if n == 0 {
		return 0, io.EOF
	}
	return n, nil
}

func (r *objectReader) Close() error {
	r.s.release()
	return nil
}
