//go:build windows

package switchsaves

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"GameSaver/internal/domain"
	"GameSaver/internal/switchmtp"
)

// ExtractToDir unpacks a console backup archive into a local directory,
// merging over what's already there. Returns files written and total bytes.
//
// Nothing here touches the console, so this is the safe way to get a Switch
// save onto the PC (and the building block the console restore uses).
func ExtractToDir(archivePath, destDir string) (int, int64, error) {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return 0, 0, fmt.Errorf("архив: %w", err)
	}
	defer zr.Close()
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return 0, 0, err
	}
	absDest, err := filepath.Abs(destDir)
	if err != nil {
		return 0, 0, err
	}
	var files int
	var total int64
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		target := filepath.Join(destDir, filepath.FromSlash(f.Name))
		absTarget, err := filepath.Abs(target)
		if err != nil {
			return files, total, err
		}
		// Zip-slip guard, same rule as backup.unzipInto.
		if absTarget != absDest && !strings.HasPrefix(absTarget, absDest+string(filepath.Separator)) {
			return files, total, fmt.Errorf("архив пытается писать вне папки: %s", f.Name)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return files, total, err
		}
		rc, err := f.Open()
		if err != nil {
			return files, total, err
		}
		out, err := os.Create(target)
		if err != nil {
			rc.Close()
			return files, total, err
		}
		n, err := io.Copy(out, rc)
		rc.Close()
		closeErr := out.Close()
		if err != nil {
			return files, total, err
		}
		if closeErr != nil {
			return files, total, closeErr
		}
		files++
		total += n
	}
	return files, total, nil
}

// RestoreToConsole writes a console backup back onto the Switch, optionally
// onto a different profile than the one it was captured from (restoring a
// save from one account onto another is the whole point of the profile picker).
//
// 🔴 Takes an unconditional backup of the destination profile first; if that
// fails, nothing is written. Requires a writable session.
//
// The archive is staged into a temp directory rather than streamed straight
// from the zip, because Upload has to know each file's exact size up front
// (MTP requires it) and needs to compare the whole set against the device to
// decide what to delete.
func RestoreToConsole(
	s *switchmtp.Session,
	dev switchmtp.Device,
	backupRoot, archivePath, title, profile, appVersion string,
	onProgress func(Progress),
) (*UploadPlan, error) {
	// Fail early if the destination doesn't exist, before we spend time on a
	// backup: DBI only exposes profiles that actually hold save data for the
	// title, and we cannot conjure a new one.
	if _, err := FindEntry(s, title, profile); err != nil {
		return nil, err
	}

	if onProgress != nil {
		onProgress(Progress{Total: 1, Title: "бэкап " + title + " / " + profile})
	}
	if _, err := BackupEntry(s, dev, backupRoot, title, profile, domain.TriggerPreRest, appVersion); err != nil {
		return nil, fmt.Errorf("резервная копия сейва на консоли не удалась, восстановление отменено: %w", err)
	}

	tmp, err := os.MkdirTemp("", "gs-switch-restore-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	if _, _, err := ExtractToDir(archivePath, tmp); err != nil {
		return nil, err
	}
	return Upload(s, tmp, title, profile, onProgress)
}

// TitleEntries lists the profiles / save-data types that exist for one title,
// so the restore dialog can offer real destinations instead of free text.
func TitleEntries(s *switchmtp.Session, title string) ([]Entry, error) {
	saves, err := findSavesStore(s)
	if err != nil {
		return nil, err
	}
	groups, err := s.Children(saves.ID)
	if err != nil {
		return nil, err
	}
	for _, g := range groups {
		t, ok, err := s.Child(g.ID, title)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		kids, err := s.Children(t.ID)
		if err != nil {
			return nil, err
		}
		out := []Entry{}
		for _, k := range kids {
			if !k.IsDir {
				continue
			}
			e := Entry{Name: k.Name, ObjectID: k.ID, Kind: classify(k.Name)}
			files, err := s.Walk(k.ID)
			if err != nil {
				return nil, err
			}
			e.FileCount = len(files)
			for _, f := range files {
				e.SizeBytes += f.Size
			}
			out = append(out, e)
		}
		return out, nil
	}
	return nil, fmt.Errorf("игра не найдена на консоли: %s", title)
}
