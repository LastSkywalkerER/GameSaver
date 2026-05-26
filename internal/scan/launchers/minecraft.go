//go:build windows

package launchers

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"GameSaver/internal/domain"
	"GameSaver/internal/util"
)

// MinecraftScanner detects multiple Minecraft launchers a user may have side by side.
type MinecraftScanner struct{}

func (MinecraftScanner) Name() string { return "minecraft" }

func (MinecraftScanner) Scan(ctx context.Context) ([]*ScanItem, error) {
	out := []*ScanItem{}
	appData := os.Getenv("APPDATA")
	localApp := os.Getenv("LOCALAPPDATA")

	type cand struct{ Dir, Label string }
	candidates := []cand{
		{filepath.Join(appData, ".minecraft"), "Minecraft (.minecraft)"},
		{filepath.Join(localApp, "Packages"), ""}, // Microsoft Store edition lives here
	}
	// XMCL and similar live on H: by name in the user's case; also check Documents and a few drives.
	for _, drive := range windowsFixedDrives() {
		candidates = append(candidates,
			cand{filepath.Join(drive, "X Minecraft Launcher"), "X Minecraft Launcher"},
			cand{filepath.Join(drive, "xmcl"), "XMCL"},
			cand{filepath.Join(drive, "SkyLauncher"), "SkyLauncher"},
		)
	}
	now := time.Now().Unix()
	for _, c := range candidates {
		if ctx.Err() != nil {
			return out, nil
		}
		if !util.IsDir(c.Dir) {
			continue
		}
		exe := findPrimaryExe(c.Dir, "minecraft")
		if exe == "" {
			// many launchers don't ship an exe; record as install without exe
			exe = filepath.Join(c.Dir, "_no_exe_")
		}
		label := c.Label
		if label == "" {
			label = "Minecraft (" + filepath.Base(c.Dir) + ")"
		}
		st, _ := os.Stat(exe)
		var sz int64
		if st != nil {
			sz = st.Size()
		}
		out = append(out, &ScanItem{
			Name:       label,
			Source:     domain.SourceMinecraft,
			RootPath:   c.Dir,
			ExePath:    exe,
			SizeBytes:  sz,
			InstallID:  util.InstallationID(exe),
			GameKey:    "minecraft",
			LastSeenAt: now,
		})
	}
	return out, nil
}
