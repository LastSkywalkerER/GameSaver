// Package power wraps the Win32 calls for "lock screen" and "sleep" so
// the shell-mode UI can let the user park their PC without leaving
// GameSaver. Both are pure user32 / powrprof calls — no admin needed.
package power

import (
	"fmt"
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

var (
	user32              = syscall.NewLazyDLL("user32.dll")
	procLockWorkStation = user32.NewProc("LockWorkStation")

	powrprof            = syscall.NewLazyDLL("powrprof.dll")
	procSetSuspendState = powrprof.NewProc("SetSuspendState")
)

// Lock invokes the Win32 LockWorkStation API — the user is bounced to
// the lock screen (same effect as Win+L) and has to enter their password
// / PIN to come back. GameSaver keeps running in the background.
func Lock() error {
	r, _, e := procLockWorkStation.Call()
	if r == 0 {
		return fmt.Errorf("LockWorkStation: %w", e)
	}
	return nil
}

// Sleep puts the PC into S3 standby and BLOCKS until the system resumes.
//
// We call SetSuspendState directly rather than via
// `rundll32 powrprof.dll,SetSuspendState` because rundll32's entry point
// ignores its string arguments and always sleeps with wake events
// ENABLED — which is the classic "PC wakes up two seconds after sleeping"
// bug: a wake-armed device (wireless controller dongle, mouse, NIC) fires
// an event during the suspend transition and bounces the machine right
// back awake.
//
// Direct call lets us pass bWakeUpEventsDisabled=TRUE: the system
// suspends with wake events disabled, so spurious device activity can't
// immediately re-wake it. The hardware power button still wakes the PC
// (it's not a "wake event" in this sense). Applies only to this suspend.
//
// SetSuspendState needs SE_SHUTDOWN_NAME; we enable it best-effort first.
func Sleep() error {
	enableShutdownPrivilege()

	// SetSuspendState(bHibernate=FALSE, bForce=FALSE, bWakeUpEventsDisabled=TRUE)
	r, _, err := procSetSuspendState.Call(0, 0, 1)
	if r != 0 {
		return nil // returns after the machine resumes
	}
	// Fallback: the direct call failed (rare) — use rundll32. Can't control
	// wake events this way, but at least it sleeps.
	if e := exec.Command("rundll32.exe", "powrprof.dll,SetSuspendState", "0,1,0").Start(); e != nil {
		return fmt.Errorf("SetSuspendState failed (%v) and rundll32 fallback failed: %w", err, e)
	}
	return nil
}

// enableShutdownPrivilege turns on SE_SHUTDOWN_NAME for our process token
// so SetSuspendState is allowed. Best-effort — errors are swallowed since
// the rundll32 fallback path handles the case where we can't get it.
func enableShutdownPrivilege() {
	var tok windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(),
		windows.TOKEN_ADJUST_PRIVILEGES|windows.TOKEN_QUERY, &tok); err != nil {
		return
	}
	defer tok.Close()

	var luid windows.LUID
	name, _ := windows.UTF16PtrFromString("SeShutdownPrivilege")
	if err := windows.LookupPrivilegeValue(nil, name, &luid); err != nil {
		return
	}
	tp := windows.Tokenprivileges{
		PrivilegeCount: 1,
	}
	tp.Privileges[0] = windows.LUIDAndAttributes{
		Luid:       luid,
		Attributes: windows.SE_PRIVILEGE_ENABLED,
	}
	_ = windows.AdjustTokenPrivileges(tok, false, &tp, 0, nil, nil)
}
