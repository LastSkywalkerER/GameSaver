//go:build windows

package launchers

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/windows/registry"

	"GameSaver/internal/domain"
	"GameSaver/internal/util"
)

// SteamScanner discovers Steam-installed games via libraryfolders.vdf and per-app manifests.
type SteamScanner struct{}

func (SteamScanner) Name() string { return "steam" }

// Scan returns Installations + a name lookup map AppID->Name.
func (s SteamScanner) Scan(ctx context.Context) ([]*ScanItem, error) {
	steamRoot, err := steamInstallPath()
	if err != nil {
		slog.Info("steam not found", "err", err)
		return nil, nil
	}
	libFile := filepath.Join(steamRoot, "steamapps", "libraryfolders.vdf")
	libs, err := readLibraryFolders(libFile)
	if err != nil {
		slog.Warn("read libraryfolders", "err", err)
		libs = []string{steamRoot}
	}

	out := []*ScanItem{}
	for _, lib := range libs {
		manifests, _ := filepath.Glob(filepath.Join(lib, "steamapps", "appmanifest_*.acf"))
		for _, mf := range manifests {
			if ctx.Err() != nil {
				return out, nil
			}
			item := parseAppManifest(mf, lib)
			if item != nil {
				out = append(out, item)
			}
		}
	}
	return out, nil
}

func steamInstallPath() (string, error) {
	for _, key := range []string{`SOFTWARE\Valve\Steam`, `SOFTWARE\WOW6432Node\Valve\Steam`} {
		k, err := registry.OpenKey(registry.LOCAL_MACHINE, key, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		v, _, err := k.GetStringValue("InstallPath")
		k.Close()
		if err == nil && v != "" {
			return v, nil
		}
	}
	return "", fmt.Errorf("steam not installed")
}

// readLibraryFolders parses libraryfolders.vdf and returns the list of library root paths.
func readLibraryFolders(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	text := string(data)
	libs := []string{}
	// extract all "path"  "..."  occurrences at any depth
	for {
		idx := strings.Index(text, `"path"`)
		if idx < 0 {
			break
		}
		rest := text[idx+len(`"path"`):]
		// find next quoted value
		q1 := strings.Index(rest, `"`)
		if q1 < 0 {
			break
		}
		rest = rest[q1+1:]
		q2 := strings.Index(rest, `"`)
		if q2 < 0 {
			break
		}
		val := rest[:q2]
		val = strings.ReplaceAll(val, `\\`, `\`)
		libs = append(libs, val)
		text = rest[q2+1:]
	}
	return libs, nil
}

func parseAppManifest(mf, libRoot string) *ScanItem {
	data, err := os.ReadFile(mf)
	if err != nil {
		return nil
	}
	kv := parseACF(string(data))
	appID := kv["appid"]
	name := kv["name"]
	installDir := kv["installdir"]
	if appID == "" || name == "" || installDir == "" {
		return nil
	}
	// Filter out Steam's pseudo-apps that aren't games. AppIDs known to be
	// runtimes/redistributables/dedicated-servers can be added here as we find them.
	switch appID {
	case "228980": // Steamworks Common Redistributables
		return nil
	}
	if strings.Contains(strings.ToLower(name), "dedicated server") {
		return nil
	}
	root := filepath.Join(libRoot, "steamapps", "common", installDir)
	if !util.IsDir(root) {
		return nil
	}
	// Steam already tells us this is an installed game — don't reject just
	// because our exe-picker can't find a high-scoring one. Use whatever exe we
	// find (best-effort), and fall back to the install root if there's none.
	exe := findPrimaryExeLenient(root, name)
	idSeed := exe
	if idSeed == "" {
		idSeed = root
	}
	appIDNum, _ := strconv.ParseInt(appID, 10, 64)
	var sz int64
	if exe != "" {
		if st, err := os.Stat(exe); err == nil {
			sz = st.Size()
		}
	}
	return &ScanItem{
		Name:        name,
		Source:      domain.SourceSteam,
		SourceAppID: appID,
		SteamAppID:  appIDNum,
		RootPath:    root,
		ExePath:     exe,
		SizeBytes:   sz,
		LaunchURI:   fmt.Sprintf("steam://rungameid/%s", appID),
		InstallID:   util.InstallationID(idSeed),
		LastSeenAt:  time.Now().Unix(),
	}
}

// parseACF parses Valve KeyValues into a flat top-level map.
// Good enough for AppState files; nested values are concatenated by last key.
func parseACF(s string) map[string]string {
	out := map[string]string{}
	lines := strings.Split(s, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, `"`) {
			continue
		}
		// "key"\t\t"value"
		// quick parser:
		rest := line[1:]
		k1 := strings.Index(rest, `"`)
		if k1 < 0 {
			continue
		}
		key := strings.ToLower(rest[:k1])
		rest = rest[k1+1:]
		v1 := strings.Index(rest, `"`)
		if v1 < 0 {
			continue
		}
		rest = rest[v1+1:]
		v2 := strings.Index(rest, `"`)
		if v2 < 0 {
			continue
		}
		val := rest[:v2]
		val = strings.ReplaceAll(val, `\\`, `\`)
		out[key] = val
	}
	return out
}
