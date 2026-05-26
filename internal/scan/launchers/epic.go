//go:build windows

package launchers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"GameSaver/internal/domain"
	"GameSaver/internal/util"
)

// EpicScanner reads Epic Games Launcher manifests under %PROGRAMDATA%\Epic.
type EpicScanner struct{}

func (EpicScanner) Name() string { return "epic" }

type epicManifest struct {
	DisplayName      string   `json:"DisplayName"`
	InstallLocation  string   `json:"InstallLocation"`
	LaunchExecutable string   `json:"LaunchExecutable"`
	CatalogNamespace string   `json:"CatalogNamespace"`
	CatalogItemId    string   `json:"CatalogItemId"`
	AppName          string   `json:"AppName"`
	MainGameAppName  string   `json:"MainGameAppName"`
	BIsApplication   bool     `json:"bIsApplication"`
	AppCategories    []string `json:"AppCategories"`
}

func (EpicScanner) Scan(ctx context.Context) ([]*ScanItem, error) {
	progData := os.Getenv("PROGRAMDATA")
	if progData == "" {
		progData = `C:\ProgramData`
	}
	dir := filepath.Join(progData, "Epic", "EpicGamesLauncher", "Data", "Manifests")
	files, err := filepath.Glob(filepath.Join(dir, "*.item"))
	if err != nil || len(files) == 0 {
		return nil, nil
	}

	// Pre-pass: read all manifests, then keep only one per InstallLocation,
	// preferring the main game (not a DLC / addon).
	type parsed struct {
		path string
		m    epicManifest
	}
	all := make([]parsed, 0, len(files))
	for _, f := range files {
		if ctx.Err() != nil {
			return nil, nil
		}
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var m epicManifest
		if err := json.Unmarshal(data, &m); err != nil {
			continue
		}
		if m.DisplayName == "" || m.InstallLocation == "" {
			continue
		}
		all = append(all, parsed{path: f, m: m})
	}

	// Group by InstallLocation (case-insensitive); keep the "main game" candidate.
	byLoc := map[string]parsed{}
	for _, p := range all {
		key := strings.ToLower(filepath.Clean(p.m.InstallLocation))
		if cur, ok := byLoc[key]; ok {
			if scoreMain(p.m) > scoreMain(cur.m) {
				byLoc[key] = p
			}
		} else {
			byLoc[key] = p
		}
	}

	out := []*ScanItem{}
	for _, p := range byLoc {
		m := p.m
		if isAddon(m) {
			continue
		}
		exe := filepath.Join(m.InstallLocation, m.LaunchExecutable)
		if m.LaunchExecutable == "" || !util.PathExists(exe) {
			exe = findPrimaryExe(m.InstallLocation, m.DisplayName)
		}
		if exe == "" {
			continue
		}
		st, _ := os.Stat(exe)
		var sz int64
		if st != nil {
			sz = st.Size()
		}
		launch := ""
		if m.CatalogNamespace != "" && m.CatalogItemId != "" && m.AppName != "" {
			launch = fmt.Sprintf("com.epicgames.launcher://apps/%s%%3A%s%%3A%s?action=launch&silent=true",
				url.PathEscape(m.CatalogNamespace), url.PathEscape(m.CatalogItemId), url.PathEscape(m.AppName))
		}
		out = append(out, &ScanItem{
			Name:        m.DisplayName,
			Source:      domain.SourceEpic,
			SourceAppID: m.AppName,
			RootPath:    m.InstallLocation,
			ExePath:     exe,
			SizeBytes:   sz,
			LaunchURI:   launch,
			InstallID:   util.InstallationID(exe),
			LastSeenAt:  time.Now().Unix(),
		})
	}
	return out, nil
}

// isAddon returns true if the manifest is a DLC / cosmetic add-on rather than
// the playable main entry.
func isAddon(m epicManifest) bool {
	if m.LaunchExecutable == "" {
		return true
	}
	if m.MainGameAppName != "" && m.MainGameAppName != m.AppName {
		return true
	}
	for _, c := range m.AppCategories {
		if strings.EqualFold(c, "addons") {
			return true
		}
	}
	return false
}

// scoreMain ranks how "main-game-like" a manifest is. Highest wins per location.
func scoreMain(m epicManifest) int {
	s := 0
	if m.LaunchExecutable != "" {
		s += 5
	}
	if m.MainGameAppName == "" || m.MainGameAppName == m.AppName {
		s += 3
	}
	for _, c := range m.AppCategories {
		switch strings.ToLower(c) {
		case "games":
			s += 2
		case "public":
			s += 1
		case "addons":
			s -= 5
		}
	}
	return s
}
