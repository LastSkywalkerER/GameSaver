// Package autologin helps the user configure Windows passwordless logon
// WITHOUT us ever touching their password.
//
// Why this is the right shape:
//   – On Win10 + Win11 ≤ 21H2 the netplwiz UI ("control userpasswords2")
//     ships a checkbox "Users must enter a user name and password to use
//     this computer". Unchecking it + entering password = autologon.
//     Windows stores the password in LSA secrets (encrypted), much safer
//     than the legacy plain-text registry approach.
//   – On Win11 22H2+ that checkbox is HIDDEN by default unless
//     HKLM\SOFTWARE\Microsoft\Windows NT\CurrentVersion\PasswordLess\Device
//     DevicePasswordLessBuildVersion is set to 0 (default is 2).
//
// We just unhide the checkbox (requires one UAC prompt) and open
// netplwiz. The user does the actual configure-and-type-password dance
// in the OS UI — we never see their credentials.
package autologin

import (
	"errors"
	"os/exec"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows/registry"
)

const (
	pwLessKey     = `SOFTWARE\Microsoft\Windows NT\CurrentVersion\PasswordLess\Device`
	pwLessValName = `DevicePasswordLessBuildVersion`
	pwLessUnhide  = uint64(0) // value that re-enables the checkbox
)

// CheckboxHidden returns true if the Win11 22H2+ netplwiz checkbox is
// currently hidden (DevicePasswordLessBuildVersion != 0). On older
// Windows the key may not exist — we treat that as "not hidden".
func CheckboxHidden() (bool, error) {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, pwLessKey, registry.QUERY_VALUE)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	defer k.Close()
	v, _, err := k.GetIntegerValue(pwLessValName)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	return v != pwLessUnhide, nil
}

// UnhideCheckbox launches `reg.exe` via UAC ("runas" verb) to flip
// DevicePasswordLessBuildVersion → 0. Returns nil if the user OK'd the
// UAC prompt, error if they cancelled or the call failed. No-op (returns
// nil) if the checkbox is already visible.
func UnhideCheckbox() error {
	hidden, err := CheckboxHidden()
	if err != nil {
		return err
	}
	if !hidden {
		return nil
	}
	return runElevated("reg.exe", `add "HKLM\`+pwLessKey+`" /v `+pwLessValName+` /t REG_DWORD /d 0 /f`)
}

// OpenNetplwiz launches the Windows User Accounts dialog. No elevation
// required — netplwiz prompts for elevation itself if it needs it.
func OpenNetplwiz() error {
	return exec.Command("netplwiz.exe").Start()
}

// ─── UAC elevation via ShellExecuteEx("runas") ────────────────────────

var (
	shell32             = syscall.NewLazyDLL("shell32.dll")
	procShellExecuteExW = shell32.NewProc("ShellExecuteExW")
)

type shellExecuteInfoW struct {
	cbSize         uint32
	fMask          uint32
	hwnd           uintptr
	lpVerb         *uint16
	lpFile         *uint16
	lpParameters   *uint16
	lpDirectory    *uint16
	nShow          int32
	hInstApp       uintptr
	lpIDList       uintptr
	lpClass        *uint16
	hkeyClass      uintptr
	dwHotKey       uint32
	hIconOrMonitor uintptr
	hProcess       uintptr
}

const (
	seeMaskNoCloseProcess = 0x00000040
	seeMaskNoAsync        = 0x00000100
	swShowNormal          = 1
)

func runElevated(file, params string) error {
	verbW, _ := syscall.UTF16PtrFromString("runas")
	fileW, _ := syscall.UTF16PtrFromString(file)
	paramsW, _ := syscall.UTF16PtrFromString(params)

	info := shellExecuteInfoW{
		fMask:  seeMaskNoCloseProcess | seeMaskNoAsync,
		lpVerb: verbW,
		lpFile: fileW,
		lpParameters: paramsW,
		nShow:  swShowNormal,
	}
	info.cbSize = uint32(unsafe.Sizeof(info))
	r, _, e := procShellExecuteExW.Call(uintptr(unsafe.Pointer(&info)))
	if r == 0 {
		return e // ERROR_CANCELLED if the user declined UAC
	}
	return nil
}
