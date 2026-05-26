package match

import (
	"os"
	"path/filepath"
	"strings"

	"GameSaver/internal/util"
)

func osGetenv(k string) string { return os.Getenv(k) }

func joinIfPresent(parts ...string) string {
	for _, p := range parts {
		if p == "" {
			return ""
		}
	}
	return filepath.Join(parts...)
}

// walkDeepScan recursively scans up to maxDepth levels under root and pushes
// folder candidates whose name shares tokens with the game name.
func walkDeepScan(root string, gameTokens []string, existing map[string]bool, out *[]*DeepScanResult, depth, maxDepth int) {
	if depth > maxDepth || !util.IsDir(root) {
		return
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		low := strings.ToLower(name)
		if strings.HasPrefix(low, ".") || strings.HasPrefix(low, "$") {
			continue
		}
		if isNoiseVendorDir(low) {
			continue
		}
		dir := filepath.Join(root, name)
		if existing[strings.ToLower(filepath.Clean(dir))] {
			continue
		}
		folderTokens := uniqueTokens(tokenize(name))
		score, reason := scoreFolderName(gameTokens, folderTokens)
		if score >= 30 {
			size, count, mtime := util.DirSizeAndCount(dir)
			if count > 0 && size > 0 && size < maxSaveBytes {
				*out = append(*out, &DeepScanResult{
					Path: dir, FileCount: count, SizeBytes: size, Mtime: mtime,
					Score: score, Reason: reason,
				})
			}
		}
		// Recurse one more level to catch Publisher\Game layouts.
		walkDeepScan(dir, gameTokens, existing, out, depth+1, maxDepth)
	}
}

func scoreFolderName(want, have []string) (int, string) {
	if len(want) == 0 || len(have) == 0 {
		return 0, ""
	}
	common := setIntersectCount(want, have)
	if common == 0 {
		return 0, ""
	}
	// Score formula: 30 per matching token (capped), with a small bonus when
	// the folder's tokens are a subset of (or equal to) the game's tokens.
	score := common * 30
	if setSubset(have, want) || setSubset(want, have) {
		score += 15
	}
	if len(have) == len(want) && common == len(want) {
		score += 30
	}
	if score > 200 {
		score = 200
	}
	return score, "shared " + strings.Join(intersection(want, have), ", ")
}

func intersection(a, b []string) []string {
	bs := map[string]bool{}
	for _, x := range b {
		bs[x] = true
	}
	out := []string{}
	seen := map[string]bool{}
	for _, x := range a {
		if seen[x] {
			continue
		}
		seen[x] = true
		if bs[x] {
			out = append(out, x)
		}
	}
	return out
}

func isNoiseVendorDir(low string) bool {
	for _, v := range []string{
		"adobe", "autodesk", "microsoft", "google", "mozilla",
		"nvidia", "intel", "amd", "vmware", "jetbrains",
		"packages", "temp", "crashdumps", "cef", "easyanticheat",
		"comms", "connecteddevicesplatform", "d3dscache", "amdmodernui",
		"discord", "telegram", "anydesk", "parsec", "obsidian",
	} {
		if low == v {
			return true
		}
	}
	return false
}
