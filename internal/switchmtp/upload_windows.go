//go:build windows

package switchmtp

import (
	"errors"
	"fmt"
	"io"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ErrReadOnly is returned when a write is attempted on a session that was
// opened without writable=true. Sessions used for scanning and backup ask the
// driver for GENERIC_READ only, so this is a programming-error guard rather
// than a permission check — it fires before any COM call reaches the console.
var ErrReadOnly = errors.New("switchmtp: сессия открыта только на чтение")

var (
	clsidPropVariantCollection = guid("{08A99E2F-6D6D-4B80-AF5A-BAF2BCBE4CB9}")
	iidPropVariantCollection   = guid("{89B2E422-4F1B-4316-BCEF-A44AFEA83EB3}")
)

// propVariant mirrors the Win32 PROPVARIANT. On x64 the trailing union is 16
// bytes wide, so the struct is 24 bytes total; the padding field is required
// for that size and must not be removed, or Delete() reads garbage past the
// end of our allocation.
type propVariant struct {
	vt         uint16
	r1, r2, r3 uint16
	val        uintptr
	_          uintptr
}

const vtLPWSTR = 31

// CreateFolder creates a subfolder under parentID and returns its object id.
func (s *Session) CreateFolder(parentID, name string) (string, error) {
	if !s.writable {
		return "", ErrReadOnly
	}
	vals, err := newDeviceValues()
	if err != nil {
		return "", err
	}
	defer vals.release()
	if err := vals.setString(keyObjectParentID, parentID); err != nil {
		return "", err
	}
	if err := vals.setString(keyObjectName, name); err != nil {
		return "", err
	}
	if err := vals.setGUID(keyObjectContent, contentTypeFolder); err != nil {
		return "", err
	}
	var newID *uint16
	// IPortableDeviceContent::CreateObjectWithPropertiesOnly — vtable slot 6.
	if err := hr(call(s.content, 6, uintptr(vals), uintptr(unsafe.Pointer(&newID))),
		"CreateObjectWithPropertiesOnly"); err != nil {
		return "", err
	}
	return takeString(newID), nil
}

// CreateFile streams exactly size bytes from r into a new object under
// parentID. MTP requires the size up front, which is why callers must know it
// before starting — a short or long reader corrupts the object.
func (s *Session) CreateFile(parentID, name string, size int64, r io.Reader) (string, error) {
	if !s.writable {
		return "", ErrReadOnly
	}
	vals, err := newDeviceValues()
	if err != nil {
		return "", err
	}
	defer vals.release()
	if err := vals.setString(keyObjectParentID, parentID); err != nil {
		return "", err
	}
	if err := vals.setString(keyObjectName, name); err != nil {
		return "", err
	}
	if err := vals.setString(keyObjectOrigName, name); err != nil {
		return "", err
	}
	if err := vals.setUint64(keyObjectSize, uint64(size)); err != nil {
		return "", err
	}
	if err := vals.setGUID(keyObjectContent, contentTypeGenericFile); err != nil {
		return "", err
	}
	if err := vals.setGUID(keyObjectFormat, objectFormatUnspecified); err != nil {
		return "", err
	}

	var stream uintptr
	var optimal uint32
	// IPortableDeviceContent::CreateObjectWithPropertiesAndData — slot 7.
	if err := hr(call(s.content, 7,
		uintptr(vals),
		uintptr(unsafe.Pointer(&stream)),
		uintptr(unsafe.Pointer(&optimal)),
		0), "CreateObjectWithPropertiesAndData"); err != nil {
		return "", err
	}
	cs := comStream(stream)
	defer cs.release()

	// Write in the driver's preferred block size; MTP throughput collapses if
	// this is much smaller than the optimal transfer unit.
	bufSize := int(optimal)
	if bufSize <= 0 {
		bufSize = 256 * 1024
	}
	buf := make([]byte, bufSize)
	written, err := io.CopyBuffer(cs, io.LimitReader(r, size), buf)
	if err != nil {
		return "", fmt.Errorf("запись %s: %w", name, err)
	}
	if written != size {
		return "", fmt.Errorf("запись %s: передано %d из %d байт", name, written, size)
	}
	// 🔴 Without Commit the object exists but is truncated/empty — MTP writes
	// are not durable until the stream is committed.
	if err := cs.commit(); err != nil {
		return "", err
	}
	return "", nil
}

// Delete removes objects by id, recursing into folders. This is the only
// destructive operation in the package; everything above it is additive.
func (s *Session) Delete(objectIDs ...string) error {
	if !s.writable {
		return ErrReadOnly
	}
	if len(objectIDs) == 0 {
		return nil
	}
	var coll uintptr
	if err := coCreateInstance(&clsidPropVariantCollection, &iidPropVariantCollection, &coll); err != nil {
		return fmt.Errorf("create PropVariantCollection: %w", err)
	}
	defer release(coll)

	// Keep the UTF-16 buffers alive until the Delete call returns — the
	// collection stores the pointers, it does not copy the strings.
	keep := make([]*uint16, 0, len(objectIDs))
	for _, id := range objectIDs {
		p, err := windows.UTF16PtrFromString(id)
		if err != nil {
			return err
		}
		keep = append(keep, p)
		pv := propVariant{vt: vtLPWSTR, val: uintptr(unsafe.Pointer(p))}
		if err := hr(call(coll, 5, uintptr(unsafe.Pointer(&pv))), "PropVariantCollection.Add"); err != nil {
			return err
		}
	}
	const deleteWithRecursion = 1
	err := hr(call(s.content, 8, uintptr(deleteWithRecursion), coll, 0), "Content.Delete")
	// The collection stored raw pointers into `keep` rather than copying the
	// strings, so those buffers must outlive the Delete call.
	runtime.KeepAlive(keep)
	return err
}
