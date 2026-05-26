//go:build windows

package heuristic

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"GameSaver/internal/domain"
	"GameSaver/internal/scan/launchers"
	"GameSaver/internal/util"

	"golang.org/x/sys/windows"
)

// HotZones are dirs that commonly hold games. The disk walker visits only their
// immediate subdirectories — we don't recurse the whole drive blindly.
var hotZoneNames = []string{
	"Games", "SteamLibrary", "GOG Games", "Epic Games", "Origin Games",
	"Ubisoft Games", "Battle.net", "XboxGames",
}

// LauncherNoise lists exes that look like games but are launchers/utilities.
var launcherNoiseExeBasename = map[string]bool{
	"steam.exe": true, "steamservice.exe": true, "steamwebhelper.exe": true,
	"galaxyclient.exe": true, "epicgameslauncher.exe": true,
	"eadesktop.exe": true, "ealauncher.exe": true, "originwebhelperservice.exe": true,
	"ubisoftconnect.exe": true, "upc.exe": true,
	"battle.net launcher.exe": true, "battle.net.exe": true,
	"riotclientservices.exe": true,
	"curseforge.exe": true, "overwolf.exe": true, "launchbox.exe": true,
}

// utilityDirNames are top-level folders we never treat as a "game".
// All keys are lowercase; the caller normalizes folder names before lookup.
var utilityDirNames = map[string]bool{
	// Launcher installs
	"steam":                true,
	"steamlibrary":         true,
	"galaxyclient":         true,
	"gog galaxy":           true,
	"epic games launcher":  true,
	"epicgameslauncher":    true,
	"ubisoft connect":      true,
	"ubisoft game launcher": true,
	"ea desktop":           true,
	"ea":                   true,
	"battle.net":           true,
	"riot games":           true,
	"riot client":          true,
	// Library managers / mod platforms
	"curseforge":          true,
	"curseforge windows":  true,
	"overwolf":            true,
	"launchbox":           true,
	// Mod / cheat / save tools and stash folders
	"mods":      true,
	"saves":     true,
	"savegame":  true,
	"gamesave":  true,
	"backup":    true,
	"backups":   true,
	"psp":       true,
	"emulators": true,
	"emulator":  true,
}

// utilityDirPrefixes catches names like "Nitrox_1.6.0.0", "DarkBot_xxx" etc.
var utilityDirPrefixes = []string{
	"nitrox_", "darkbot", "trainer", "savescummer",
}

// isLauncherDir returns true if dir contains a known launcher exe at depth ≤ 2.
func isLauncherDir(dir string) bool {
	markers := []string{
		"Steam.exe", "GalaxyClient.exe", "EpicGamesLauncher.exe",
		"EADesktop.exe", "UbisoftConnect.exe", "Battle.net Launcher.exe",
		"RiotClientServices.exe",
	}
	for _, m := range markers {
		for _, candidate := range []string{
			filepath.Join(dir, m),
			filepath.Join(dir, "bin", m),
		} {
			if _, err := os.Stat(candidate); err == nil {
				return true
			}
		}
	}
	return false
}

// Scanner walks fixed disks looking for standalone game installs not covered by launcher scanners.
type Scanner struct {
	// Known contains exePath (lowercased) we should skip because launchers already covered them.
	Known map[string]bool
	// KnownRoots contains RootPath (lowercased + filepath.Clean) we should skip likewise.
	KnownRoots map[string]bool
}

func New() *Scanner {
	return &Scanner{Known: map[string]bool{}, KnownRoots: map[string]bool{}}
}

func (s *Scanner) Name() string { return "heuristic" }

func (s *Scanner) Scan(ctx context.Context) ([]*launchers.ScanItem, error) {
	out := []*launchers.ScanItem{}
	for _, drive := range fixedDrives() {
		for _, hot := range hotZoneNames {
			if ctx.Err() != nil {
				return out, nil
			}
			root := filepath.Join(drive, hot)
			if !util.IsDir(root) {
				continue
			}
			children, _ := os.ReadDir(root)
			for _, ch := range children {
				if !ch.IsDir() {
					continue
				}
				name := ch.Name()
				if isIgnoredDir(name) {
					continue
				}
				gameRoot := filepath.Join(root, name)
				// SteamLibrary\steamapps\common is handled by Steam scanner
				if strings.EqualFold(name, "steamapps") {
					continue
				}
				lowName := strings.ToLower(name)
				if utilityDirNames[lowName] {
					continue
				}
				blocked := false
				for _, pref := range utilityDirPrefixes {
					if strings.HasPrefix(lowName, pref) {
						blocked = true
						break
					}
				}
				if blocked {
					continue
				}
				if isLauncherDir(gameRoot) {
					continue
				}
				if s.KnownRoots[strings.ToLower(filepath.Clean(gameRoot))] {
					continue
				}
				exe := findExeInRoot(gameRoot, name)
				if exe == "" {
					continue
				}
				if s.Known[strings.ToLower(exe)] {
					continue
				}
				exeBase := strings.ToLower(filepath.Base(exe))
				if launcherNoiseExeBasename[exeBase] {
					continue
				}
				st, _ := os.Stat(exe)
				var sz int64
				if st != nil {
					sz = st.Size()
				}
				out = append(out, &launchers.ScanItem{
					Name:       name,
					Source:     domain.SourceStandalone,
					RootPath:   gameRoot,
					ExePath:    exe,
					SizeBytes:  sz,
					InstallID:  util.InstallationID(exe),
					LastSeenAt: time.Now().Unix(),
				})
			}
		}
	}
	return out, nil
}

func findExeInRoot(root, name string) string {
	// Mirror of launchers.findPrimaryExe but lives in heuristic package; identical logic.
	type cand struct {
		path  string
		score int
		size  int64
	}
	cands := []cand{}
	preferredDirs := []string{"binaries", filepath.Join("binaries", "win64"), "bin", "bin64", "x64", filepath.Join("binaries", "retail")}
	rootLow := strings.ToLower(root)
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			low := strings.ToLower(d.Name())
			switch low {
			case "_commonredist", "redist", "vc_redist", "directx", "support", "dxsetup", "vcredist", "engine":
				return filepath.SkipDir
			}
			return nil
		}
		base := strings.ToLower(d.Name())
		if !strings.HasSuffix(base, ".exe") {
			return nil
		}
		info, _ := d.Info()
		var sz int64
		if info != nil {
			sz = info.Size()
		}
		score := 0
		if sz >= 10*1024*1024 {
			score += 3
		}
		if sz < 1*1024*1024 {
			score -= 3
		}
		nm := strings.ToLower(name)
		stem := strings.TrimSuffix(base, ".exe")
		if stem == nm || strings.HasPrefix(stem, nm) || strings.HasPrefix(nm, stem) {
			score += 5
		}
		if strings.Contains(stem, "game") || strings.Contains(stem, "win64") || strings.Contains(stem, "shipping") {
			score += 2
		}
		dirLow := strings.ToLower(filepath.Dir(p))
		dirRel := strings.TrimPrefix(dirLow, rootLow)
		dirRel = strings.TrimPrefix(dirRel, string(filepath.Separator))
		for _, pd := range preferredDirs {
			if dirRel == pd || strings.HasPrefix(dirRel, pd+string(filepath.Separator)) {
				score += 2
				break
			}
		}
		for _, bad := range []string{"unins", "vc_redist", "vcredist", "setup", "installer", "crashreport", "crashsender", "errorreport", "directx", "dotnetfx", "physx", "easyanticheat", "battleye"} {
			if strings.Contains(stem, bad) {
				score -= 10
				break
			}
		}
		cands = append(cands, cand{p, score, sz})
		return nil
	})
	if len(cands) == 0 {
		return ""
	}
	best := cands[0]
	for _, c := range cands[1:] {
		if c.score > best.score || (c.score == best.score && c.size > best.size) {
			best = c
		}
	}
	// Lower threshold: if the folder has ANY non-blacklisted exe, treat it as
	// a candidate game install. Earlier we rejected score < 1 which dropped
	// older Star Wars titles, small standalone games, etc.
	if best.score <= -5 {
		return ""
	}
	return best.path
}

func isIgnoredDir(n string) bool {
	low := strings.ToLower(n)
	if strings.HasPrefix(low, "$") || strings.HasPrefix(low, ".") {
		return true
	}
	for _, bad := range []string{"system volume information", "$recycle.bin", "windows", "tmp", "temp", "node_modules", "msdownld.tmp", "config", "music", "assets"} {
		if low == bad {
			return true
		}
	}
	return false
}

func fixedDrives() []string {
	const driveFixed = 3
	mask, _ := windows.GetLogicalDrives()
	drives := []string{}
	for i := 0; i < 26; i++ {
		if mask&(1<<uint(i)) == 0 {
			continue
		}
		letter := string(rune('A' + i))
		root := letter + ":\\"
		ptr, err := windows.UTF16PtrFromString(root)
		if err != nil {
			continue
		}
		if windows.GetDriveType(ptr) == driveFixed {
			drives = append(drives, root)
		}
	}
	return drives
}
