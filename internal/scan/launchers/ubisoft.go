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

// UbisoftScanner reads Ubisoft Connect installs from registry.
type UbisoftScanner struct{}

func (UbisoftScanner) Name() string { return "ubisoft" }

func (UbisoftScanner) Scan(ctx context.Context) ([]*ScanItem, error) {
	out := []*ScanItem{}
	for _, base := range []string{`SOFTWARE\Ubisoft\Launcher\Installs`, `SOFTWARE\WOW6432Node\Ubisoft\Launcher\Installs`} {
		k, err := registry.OpenKey(registry.LOCAL_MACHINE, base, registry.READ)
		if err != nil {
			continue
		}
		ids, _ := k.ReadSubKeyNames(-1)
		k.Close()
		for _, id := range ids {
			if ctx.Err() != nil {
				return out, nil
			}
			sk, err := registry.OpenKey(registry.LOCAL_MACHINE, base+`\`+id, registry.QUERY_VALUE)
			if err != nil {
				continue
			}
			path, _, _ := sk.GetStringValue("InstallDir")
			sk.Close()
			if path == "" || !util.IsDir(path) {
				continue
			}
			name := filepath.Base(filepath.Clean(path))
			exe := findPrimaryExe(path, name)
			if exe == "" {
				continue
			}
			st, _ := os.Stat(exe)
			var sz int64
			if st != nil {
				sz = st.Size()
			}
			out = append(out, &ScanItem{
				Name:        name,
				Source:      domain.SourceUbisoft,
				SourceAppID: id,
				RootPath:    path,
				ExePath:     exe,
				SizeBytes:   sz,
				LaunchURI:   fmt.Sprintf("uplay://launch/%s/0", id),
				InstallID:   util.InstallationID(exe),
				LastSeenAt:  time.Now().Unix(),
			})
		}
	}
	return out, nil
}
