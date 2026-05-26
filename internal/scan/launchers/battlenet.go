//go:build windows

package launchers

import (
	"context"
	"os"
	"strings"
	"time"

	"GameSaver/internal/domain"
	"GameSaver/internal/util"
)

// BattleNetScanner discovers Blizzard games via the Uninstall registry,
// matching items installed by Battle.net Agent.
type BattleNetScanner struct{}

func (BattleNetScanner) Name() string { return "battlenet" }

func (BattleNetScanner) Scan(ctx context.Context) ([]*ScanItem, error) {
	out := []*ScanItem{}
	for _, e := range readUninstallEntries() {
		if ctx.Err() != nil {
			return out, nil
		}
		uninst := strings.ToLower(e.UninstallString)
		pub := strings.ToLower(e.Publisher)
		if !(strings.Contains(uninst, "battle.net") || strings.Contains(pub, "blizzard")) {
			continue
		}
		if e.InstallLocation == "" || !util.IsDir(e.InstallLocation) {
			continue
		}
		exe := findPrimaryExe(e.InstallLocation, e.DisplayName)
		if exe == "" {
			continue
		}
		st, _ := os.Stat(exe)
		var sz int64
		if st != nil {
			sz = st.Size()
		}
		out = append(out, &ScanItem{
			Name:       e.DisplayName,
			Source:     domain.SourceBattleNet,
			RootPath:   e.InstallLocation,
			ExePath:    exe,
			SizeBytes:  sz,
			InstallID:  util.InstallationID(exe),
			LastSeenAt: time.Now().Unix(),
		})
	}
	return out, nil
}
