//go:build windows

package launchers

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"GameSaver/internal/domain"
	"GameSaver/internal/util"
)

// XboxScanner discovers Xbox/Microsoft Store games installed via the Gaming Services
// to a per-drive `XboxGames` folder.
type XboxScanner struct{}

func (XboxScanner) Name() string { return "xbox" }

func (XboxScanner) Scan(ctx context.Context) ([]*ScanItem, error) {
	out := []*ScanItem{}
	drives := windowsFixedDrives()
	for _, d := range drives {
		root := filepath.Join(d, "XboxGames")
		if !util.IsDir(root) {
			continue
		}
		ents, _ := os.ReadDir(root)
		for _, e := range ents {
			if ctx.Err() != nil {
				return out, nil
			}
			if !e.IsDir() {
				continue
			}
			gameDir := filepath.Join(root, e.Name())
			// Xbox games typically have <name>\Content\<exe>
			content := filepath.Join(gameDir, "Content")
			if !util.IsDir(content) {
				content = gameDir
			}
			name := strings.TrimSpace(e.Name())
			exe := findPrimaryExe(content, name)
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
				Source:     domain.SourceXbox,
				RootPath:   gameDir,
				ExePath:    exe,
				SizeBytes:  sz,
				InstallID:  util.InstallationID(exe),
				LastSeenAt: time.Now().Unix(),
			})
		}
	}
	return out, nil
}
