//go:build windows

package main

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

// openInExplorer reveals path in Windows Explorer. For folders it opens the
// folder; for files it opens the parent folder with the file selected.
//
// Notes / pitfalls we ran into:
//   - Plain `explorer.exe <path>` IS the right command for folders, but the
//     HideWindow flag in SysProcAttr passes SW_HIDE to CreateProcess, which
//     Explorer sometimes honors by spawning hidden and never showing the window.
//     We MUST NOT set HideWindow here.
//   - For files, `explorer.exe /select,<path>` must be a SINGLE argv element
//     (no space between the comma and the path) or Explorer treats the path as
//     a separate broken flag.
//   - Explorer is happy with UTF-16 paths via CreateProcessW, so cyrillic
//     usernames work out of the box.
//   - Explorer.exe exits with a non-zero code even on success. We Start() and
//     don't Wait().
func openInExplorer(path string) error {
	if path == "" {
		return fmt.Errorf("empty path")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	st, statErr := os.Stat(abs)

	var cmd *exec.Cmd
	switch {
	case statErr == nil && st.IsDir():
		cmd = exec.Command("explorer.exe", abs)
	case statErr == nil && !st.IsDir():
		// File exists — select it inside its parent folder.
		cmd = exec.Command("explorer.exe", "/select,"+abs)
	default:
		// Path doesn't exist. Try to open the closest ancestor that does.
		parent := abs
		for {
			next := filepath.Dir(parent)
			if next == parent {
				break
			}
			parent = next
			if _, e := os.Stat(parent); e == nil {
				break
			}
		}
		if parent == "" || parent == abs {
			return fmt.Errorf("path does not exist: %s", abs)
		}
		cmd = exec.Command("explorer.exe", parent)
	}

	// Intentionally NO HideWindow / CREATE_NO_WINDOW flags.
	cmd.SysProcAttr = &syscall.SysProcAttr{}
	if err := cmd.Start(); err != nil {
		slog.Warn("openInExplorer Start failed; falling back via cmd /c start",
			"path", abs, "err", err)
		// Fallback: rundll32 shell open via cmd. start "" "<path>" reliably
		// opens folders even when explorer.exe fails to spawn directly.
		fb := exec.Command("cmd", "/c", "start", "", strings.TrimRight(abs, `\/`))
		fb.SysProcAttr = &syscall.SysProcAttr{}
		if err2 := fb.Start(); err2 != nil {
			return fmt.Errorf("explorer: %v; fallback: %w", err, err2)
		}
		// Release the process; we don't want a zombie cmd.exe.
		go fb.Wait()
	} else {
		go cmd.Wait()
	}
	return nil
}
