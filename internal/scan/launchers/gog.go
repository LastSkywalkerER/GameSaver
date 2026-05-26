//go:build windows

package launchers

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/windows/registry"

	"GameSaver/internal/domain"
	"GameSaver/internal/util"
)

// GOGScanner enumerates installed GOG games via registry.
type GOGScanner struct{}

func (GOGScanner) Name() string { return "gog" }

func (GOGScanner) Scan(ctx context.Context) ([]*ScanItem, error) {
	out := []*ScanItem{}
	for _, base := range []string{`SOFTWARE\GOG.com\Games`, `SOFTWARE\WOW6432Node\GOG.com\Games`} {
		k, err := registry.OpenKey(registry.LOCAL_MACHINE, base, registry.READ)
		if err != nil {
			continue
		}
		subs, _ := k.ReadSubKeyNames(-1)
		k.Close()
		for _, sub := range subs {
			if ctx.Err() != nil {
				return out, nil
			}
			full := base + `\` + sub
			sk, err := registry.OpenKey(registry.LOCAL_MACHINE, full, registry.QUERY_VALUE)
			if err != nil {
				continue
			}
			name, _, _ := sk.GetStringValue("gameName")
			path, _, _ := sk.GetStringValue("path")
			exeRaw, _, _ := sk.GetStringValue("exe")
			gameID, _, _ := sk.GetStringValue("gameID")
			sk.Close()
			if name == "" || path == "" {
				continue
			}
			exe := exeRaw
			if exe != "" && !filepath.IsAbs(exe) {
				exe = filepath.Join(path, exe)
			}
			if exe == "" || !util.PathExists(exe) {
				exe = findPrimaryExe(path, name)
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
			if gameID != "" {
				launch = fmt.Sprintf("goggalaxy://openGameView/%s", gameID)
			}
			out = append(out, &ScanItem{
				Name:        name,
				Source:      domain.SourceGOG,
				SourceAppID: gameID,
				RootPath:    path,
				ExePath:     exe,
				SizeBytes:   sz,
				LaunchURI:   launch,
				InstallID:   util.InstallationID(exe),
				LastSeenAt:  time.Now().Unix(),
			})
		}
	}
	return out, nil
}
