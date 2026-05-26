//go:build windows

package launchers

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// findPrimaryExe returns the most-likely game executable inside root, or "".
// The scoring matches §7.3 of the design doc.
func findPrimaryExe(root, gameName string) string {
	type cand struct {
		path  string
		score int
		size  int64
	}
	cands := []cand{}
	preferredDirs := []string{"binaries", "binaries\\win64", "bin", "bin64", "x64", "binaries\\retail", "binaries\\steamretail"}
	rootLow := strings.ToLower(root)
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			// skip massive irrelevant subtrees
			low := strings.ToLower(d.Name())
			switch low {
			case "_commonredist", "redist", "vc_redist", "directx", "support", "engine", "vcredist", "dxsetup", "_retailonly":
				return filepath.SkipDir
			}
			return nil
		}
		name := strings.ToLower(d.Name())
		if !strings.HasSuffix(name, ".exe") {
			return nil
		}
		info, _ := d.Info()
		size := int64(0)
		if info != nil {
			size = info.Size()
		}
		score := 0
		if size >= 10*1024*1024 {
			score += 3
		}
		if size < 1*1024*1024 {
			score -= 3
		}
		base := strings.TrimSuffix(name, ".exe")
		gn := strings.ToLower(gameName)
		if base == gn || strings.HasPrefix(base, gn) || strings.HasPrefix(gn, base) {
			score += 5
		}
		if strings.Contains(base, "game") || strings.Contains(base, "win64") || strings.Contains(base, "shipping") {
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
		// Black-listed names
		for _, bad := range []string{
			"unins", "vc_redist", "vcredist", "setup", "installer", "crashreport", "crashhandler",
			"crashsender", "errorreport", "directx", "dotnetfx", "physx", "redist", "easyanticheat",
			"battleye", "launcher_legacy",
		} {
			if strings.Contains(base, bad) {
				score -= 10
				break
			}
		}
		cands = append(cands, cand{p, score, size})
		return nil
	})
	if len(cands) == 0 {
		return ""
	}
	sort.Slice(cands, func(i, j int) bool {
		if cands[i].score != cands[j].score {
			return cands[i].score > cands[j].score
		}
		return cands[i].size > cands[j].size
	})
	best := cands[0]
	if best.score < 1 {
		return ""
	}
	return best.path
}

// findPrimaryExeLenient is like findPrimaryExe but returns the best non-blacklisted
// exe even when its score is low. Used by launcher scanners where we KNOW the
// folder is a game (Steam, GOG, Epic etc.) so we should always pick something.
func findPrimaryExeLenient(root, gameName string) string {
	if exe := findPrimaryExe(root, gameName); exe != "" {
		return exe
	}
	// Fallback: any .exe in root or top-level subdirs, biggest wins, excluding
	// the obvious noise (unins, redist, crash, …). Walks shallow only.
	type cand struct {
		path string
		size int64
	}
	cands := []cand{}
	root = filepath.Clean(root)
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
			// Limit depth to 3 to avoid scanning massive game data trees.
			rel, _ := filepath.Rel(root, p)
			if strings.Count(rel, string(filepath.Separator)) >= 3 {
				return filepath.SkipDir
			}
			return nil
		}
		name := strings.ToLower(d.Name())
		if !strings.HasSuffix(name, ".exe") {
			return nil
		}
		stem := strings.TrimSuffix(name, ".exe")
		for _, bad := range []string{
			"unins", "vc_redist", "vcredist", "setup", "installer",
			"crashreport", "crashsender", "errorreport",
			"directx", "dotnetfx", "physx", "easyanticheat", "battleye",
		} {
			if strings.Contains(stem, bad) {
				return nil
			}
		}
		info, _ := d.Info()
		var sz int64
		if info != nil {
			sz = info.Size()
		}
		cands = append(cands, cand{p, sz})
		return nil
	})
	if len(cands) == 0 {
		return ""
	}
	best := cands[0]
	for _, c := range cands[1:] {
		if c.size > best.size {
			best = c
		}
	}
	return best.path
}
