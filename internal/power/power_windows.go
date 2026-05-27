// Package power wraps the Win32 calls for "lock screen" and "sleep" so
// the shell-mode UI can let the user park their PC without leaving
// GameSaver. Both are pure user32 / powrprof calls — no admin needed.
package power

import (
	"fmt"
	"os/exec"
	"syscall"
)

var (
	user32              = syscall.NewLazyDLL("user32.dll")
	procLockWorkStation = user32.NewProc("LockWorkStation")
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

// Sleep puts the PC into S3 standby. SetSuspendState directly requires
// SE_SHUTDOWN_NAME privilege which is enabled but not active for normal
// user processes; the simplest reliable path is to shell out via
// rundll32 which goes through powrprof.dll's exported helper that does
// the privilege dance for us.
//
// Caveat: if "Hibernate after standby" is enabled in Windows power
// options, SetSuspendState may hibernate instead of sleep. That's
// standard Windows behaviour — the user can change it in their power
// plan.
func Sleep() error {
	// "0,1,0" = bHibernate=FALSE, bForce=TRUE, bWakeupEventsDisabled=FALSE
	return exec.Command("rundll32.exe", "powrprof.dll,SetSuspendState", "0,1,0").Start()
}
