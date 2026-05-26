package util

import (
	"os"
	"path/filepath"
)

// PathExists returns true if the path exists (file or dir) and stat succeeded.
func PathExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// IsDir returns true if path is an existing directory.
func IsDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

// DirSizeAndCount walks p and returns (totalBytes, fileCount, latestMtime).
// Symlinks/junctions are not followed beyond a single os.Stat call to avoid loops.
func DirSizeAndCount(p string) (int64, int, int64) {
	var total int64
	var count int
	var latest int64
	_ = filepath.WalkDir(p, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		total += info.Size()
		count++
		if m := info.ModTime().Unix(); m > latest {
			latest = m
		}
		return nil
	})
	return total, count, latest
}

// FirstExistingDir returns the first dir that exists from a list.
func FirstExistingDir(candidates ...string) string {
	for _, c := range candidates {
		if IsDir(c) {
			return c
		}
	}
	return ""
}
