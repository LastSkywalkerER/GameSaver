//go:build windows

package launchers

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/windows/registry"

	"GameSaver/internal/domain"
	"GameSaver/internal/util"
)

// EAScanner discovers EA Desktop games via Uninstall registry keys.
type EAScanner struct{}

func (EAScanner) Name() string { return "ea" }

func (EAScanner) Scan(ctx context.Context) ([]*ScanItem, error) {
	out := []*ScanItem{}
	for _, item := range readUninstallEntries() {
		if ctx.Err() != nil {
			return out, nil
		}
		pub := strings.ToLower(item.Publisher)
		name := item.DisplayName
		if !(strings.Contains(pub, "electronic arts") || strings.Contains(pub, "ea games") || strings.Contains(strings.ToLower(item.UninstallString), "ea desktop")) {
			continue
		}
		if item.InstallLocation == "" || name == "" {
			continue
		}
		if !util.IsDir(item.InstallLocation) {
			continue
		}
		exe := findPrimaryExe(item.InstallLocation, name)
		if exe == "" {
			continue
		}
		st, _ := os.Stat(exe)
		var sz int64
		if st != nil {
			sz = st.Size()
		}
		out = append(out, &ScanItem{
			Name:       name,
			Source:     domain.SourceEA,
			RootPath:   item.InstallLocation,
			ExePath:    exe,
			SizeBytes:  sz,
			InstallID:  util.InstallationID(exe),
			LastSeenAt: time.Now().Unix(),
		})
	}
	return out, nil
}

type uninstallEntry struct {
	DisplayName     string
	Publisher       string
	InstallLocation string
	UninstallString string
	DisplayIcon     string
}

func readUninstallEntries() []uninstallEntry {
	var out []uninstallEntry
	keys := []struct {
		Root registry.Key
		Path string
	}{
		{registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`},
		{registry.LOCAL_MACHINE, `SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall`},
		{registry.CURRENT_USER, `SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`},
	}
	for _, kp := range keys {
		k, err := registry.OpenKey(kp.Root, kp.Path, registry.READ)
		if err != nil {
			continue
		}
		subs, _ := k.ReadSubKeyNames(-1)
		k.Close()
		for _, s := range subs {
			sk, err := registry.OpenKey(kp.Root, kp.Path+`\`+s, registry.QUERY_VALUE)
			if err != nil {
				continue
			}
			var e uninstallEntry
			e.DisplayName, _, _ = sk.GetStringValue("DisplayName")
			e.Publisher, _, _ = sk.GetStringValue("Publisher")
			e.InstallLocation, _, _ = sk.GetStringValue("InstallLocation")
			e.UninstallString, _, _ = sk.GetStringValue("UninstallString")
			e.DisplayIcon, _, _ = sk.GetStringValue("DisplayIcon")
			sk.Close()
			if e.DisplayName == "" {
				continue
			}
			if e.InstallLocation == "" && e.DisplayIcon != "" {
				e.InstallLocation = filepath.Dir(strings.Split(e.DisplayIcon, ",")[0])
			}
			out = append(out, e)
		}
	}
	return out
}
