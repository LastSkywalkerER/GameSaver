package bluetooth

import (
	"testing"
	"unsafe"
)

// TestStructSizes pins the Win32 struct sizes. A drift of even one byte
// silently shifts every field after the mistake and the syscall is rejected or
// reads garbage (the devModeW 224-vs-220 incident). These sizes come from
// bluetoothapis.h on x64. Run: `go test ./internal/bluetooth/...`.
func TestStructSizes(t *testing.T) {
	cases := []struct {
		name string
		got  uintptr
		want uintptr
	}{
		{"btFindRadioParams", unsafe.Sizeof(btFindRadioParams{}), 4},
		{"btDeviceSearchParams", unsafe.Sizeof(btDeviceSearchParams{}), 40},
		{"btDeviceInfo", unsafe.Sizeof(btDeviceInfo{}), 560},
		{"btAuthCallbackParams", unsafe.Sizeof(btAuthCallbackParams{}), 576},
		{"btAuthResponse", unsafe.Sizeof(btAuthResponse{}), 48},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s = %d bytes, want %d (Win32 ABI mismatch)", c.name, c.got, c.want)
		}
	}
}
