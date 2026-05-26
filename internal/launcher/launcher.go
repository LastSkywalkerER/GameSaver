//go:build windows

package launcher

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"GameSaver/internal/domain"
	"GameSaver/internal/storage/sqlite"
)

// Service launches games by trying deep-links first and falling back to exe.
type Service struct {
	db *sqlite.Store
}

func New(db *sqlite.Store) *Service { return &Service{db: db} }

// Launch starts a game. If installationID is empty, picks the best installation.
func (s *Service) Launch(gameID, installationID string) error {
	insts, err := s.db.ListInstallations(gameID)
	if err != nil {
		return err
	}
	if len(insts) == 0 {
		return fmt.Errorf("no installations")
	}
	var chosen *domain.Installation
	if installationID != "" {
		for _, i := range insts {
			if i.ID == installationID {
				chosen = i
				break
			}
		}
	}
	if chosen == nil {
		// preferred order: steam > epic > gog > ea > ubisoft > battlenet > else first
		order := map[domain.SourceKind]int{
			domain.SourceSteam: 1, domain.SourceEpic: 2, domain.SourceGOG: 3,
			domain.SourceEA: 4, domain.SourceUbisoft: 5, domain.SourceBattleNet: 6,
		}
		bestRank := 99
		for _, i := range insts {
			r := 50
			if v, ok := order[i.Source]; ok {
				r = v
			}
			if r < bestRank {
				bestRank = r
				chosen = i
			}
		}
	}
	if chosen == nil {
		return fmt.Errorf("no installation chosen")
	}
	if chosen.LaunchURI != "" {
		if err := openURI(chosen.LaunchURI); err == nil {
			return nil
		}
	}
	if chosen.ExePath == "" || strings.HasSuffix(chosen.ExePath, "_no_exe_") {
		return fmt.Errorf("no executable for installation %s", chosen.ID)
	}
	return execProcess(chosen.ExePath, filepath.Dir(chosen.ExePath))
}

// openURI lets Windows resolve a custom protocol via the shell.
func openURI(uri string) error {
	cmd := exec.Command("cmd", "/c", "start", "", uri)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd.Start()
}

func execProcess(exePath, wd string) error {
	cmd := exec.Command(exePath)
	cmd.Dir = wd
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: false}
	return cmd.Start()
}
