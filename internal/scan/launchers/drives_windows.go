//go:build windows

package launchers

import (
	"golang.org/x/sys/windows"
)

// windowsFixedDrives returns drive roots (e.g. ["C:\\","D:\\","E:\\","H:\\"]) classified as DRIVE_FIXED.
func windowsFixedDrives() []string {
	const driveFixed = 3
	mask, _ := windows.GetLogicalDrives()
	drives := []string{}
	for i := 0; i < 26; i++ {
		if mask&(1<<uint(i)) == 0 {
			continue
		}
		letter := string(rune('A' + i))
		root := letter + ":\\"
		rootPtr, err := windows.UTF16PtrFromString(root)
		if err != nil {
			continue
		}
		t := windows.GetDriveType(rootPtr)
		if t == driveFixed {
			drives = append(drives, root)
		}
	}
	return drives
}
