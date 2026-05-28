//go:build windows

package launcher

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/windows/registry"

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

	exeOK := chosen.ExePath != "" && !strings.HasSuffix(chosen.ExePath, "_no_exe_")
	// Only consider a deep-link if its protocol actually has a handler
	// registered. cmd's `start` reports success for an UNregistered scheme
	// (the shell takes the hand-off and then pops its own "can't open this
	// link" dialog), so we can't rely on openURI's error — we must check
	// the registry up front. This is what bit GOG games: we fired
	// goggalaxy://... even though GOG Galaxy wasn't installed.
	uriOK := chosen.LaunchURI != "" && protocolRegistered(chosen.LaunchURI)

	// Steam deep-links are the canonical launch path (overlay, cloud saves,
	// DRM) and steam:// is registered whenever Steam is installed — so
	// prefer the URI for Steam. For every other source the bare exe is the
	// most reliable route (it's exactly what a desktop shortcut uses); fall
	// back to a deep-link only if there's no usable exe.
	if chosen.Source == domain.SourceSteam && uriOK {
		return openURI(chosen.LaunchURI)
	}
	if exeOK {
		return execProcess(chosen.ExePath, filepath.Dir(chosen.ExePath))
	}
	if uriOK {
		return openURI(chosen.LaunchURI)
	}
	return fmt.Errorf("no way to launch %s: no executable and %q protocol not registered",
		chosen.ID, schemeOf(chosen.LaunchURI))
}

// openURI lets Windows resolve a custom protocol via the shell.
func openURI(uri string) error {
	cmd := exec.Command("cmd", "/c", "start", "", uri)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd.Start()
}

// schemeOf returns the protocol part of a URI ("goggalaxy://x" → "goggalaxy").
func schemeOf(uri string) string {
	if i := strings.Index(uri, ":"); i > 0 {
		return uri[:i]
	}
	return uri
}

// protocolRegistered reports whether the URI's scheme has a handler
// registered in HKCR. A URL protocol key carries a "URL Protocol" value
// (per the Win32 shell convention) — its presence is the canonical marker.
func protocolRegistered(uri string) bool {
	scheme := schemeOf(uri)
	if scheme == "" {
		return false
	}
	k, err := registry.OpenKey(registry.CLASSES_ROOT, scheme, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	if _, _, err := k.GetStringValue("URL Protocol"); err == nil {
		return true
	}
	return false
}

func execProcess(exePath, wd string) error {
	cmd := exec.Command(exePath)
	cmd.Dir = wd
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: false}
	return cmd.Start()
}
