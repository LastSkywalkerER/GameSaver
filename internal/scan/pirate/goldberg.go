//go:build windows

package pirate

import (
	"os"
	"path/filepath"
	"strconv"
)

// GoldbergSaves enumerates per-AppID save folders under %APPDATA%\Goldberg SteamEmu Saves.
type GoldbergSave struct {
	AppID int64
	Path  string
}

func ListGoldbergSaves() []GoldbergSave {
	appData := os.Getenv("APPDATA")
	if appData == "" {
		return nil
	}
	base := filepath.Join(appData, "Goldberg SteamEmu Saves")
	entries, err := os.ReadDir(base)
	if err != nil {
		return nil
	}
	out := []GoldbergSave{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		id, err := strconv.ParseInt(e.Name(), 10, 64)
		if err != nil {
			continue
		}
		out = append(out, GoldbergSave{AppID: id, Path: filepath.Join(base, e.Name())})
	}
	return out
}

// FLTRoot returns the FLT save root if present.
func FLTRoot() string {
	appData := os.Getenv("APPDATA")
	if appData == "" {
		return ""
	}
	p := filepath.Join(appData, "FLT")
	if st, err := os.Stat(p); err == nil && st.IsDir() {
		return p
	}
	return ""
}
