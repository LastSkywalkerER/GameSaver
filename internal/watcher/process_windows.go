//go:build windows

package watcher

import (
	"os/exec"
	"strings"
	"syscall"
)

// isAnyInstallRunning consults tasklist.exe (built into Windows) for any
// process whose imagename equals one of the game's installations' exe basenames.
func (s *Service) isAnyInstallRunning(gameID string) bool {
	insts, err := s.db.ListInstallations(gameID)
	if err != nil {
		return false
	}
	for _, i := range insts {
		if i.ExePath == "" || strings.HasSuffix(i.ExePath, "_no_exe_") {
			continue
		}
		name := basename(i.ExePath)
		if name == "" {
			continue
		}
		if isProcessRunning(name) {
			return true
		}
	}
	return false
}

func basename(p string) string {
	if i := strings.LastIndexAny(p, `\/`); i >= 0 {
		return p[i+1:]
	}
	return p
}

// isProcessRunning shells out to tasklist with an imagename filter — much
// simpler than EnumProcesses and good enough for our infrequent polling.
func isProcessRunning(exeName string) bool {
	cmd := exec.Command("tasklist.exe", "/NH", "/FO", "CSV", "/FI", "IMAGENAME eq "+exeName)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	low := strings.ToLower(string(out))
	return strings.Contains(low, strings.ToLower(exeName))
}
