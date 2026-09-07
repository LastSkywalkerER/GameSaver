//go:build windows

package switchsaves

import (
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"GameSaver/internal/switchmtp"
)

// ─── Switch → Windows ──────────────────────────────────────────────────────

// DownloadResult summarises a console-to-PC copy.
type DownloadResult struct {
	Files      int   `json:"files"`
	TotalBytes int64 `json:"totalBytes"`
}

// DownloadTo copies one console profile's save tree into a local directory,
// merging over whatever is already there (same-named files are replaced,
// unrelated files are kept) — the same semantics as the Windows-to-Windows
// MigrateSave.
//
// Callers must have taken a snapshot of destDir first; this function will
// happily overwrite it.
func DownloadTo(s *switchmtp.Session, title, profile, destDir string) (*DownloadResult, error) {
	entry, err := FindEntry(s, title, profile)
	if err != nil {
		return nil, err
	}
	files, err := s.Walk(entry.ID)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return nil, err
	}
	res := &DownloadResult{}
	for _, f := range files {
		target := filepath.Join(destDir, filepath.FromSlash(f.Rel))
		// Guard against a malicious/odd device path escaping the destination.
		if !withinDir(destDir, target) {
			return nil, fmt.Errorf("путь вне целевой папки: %s", f.Rel)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return nil, err
		}
		rc, err := s.Open(f.ID)
		if err != nil {
			return nil, fmt.Errorf("чтение %s: %w", f.Rel, err)
		}
		// Write to a temp file and rename, so an interrupted transfer leaves
		// the previous version intact rather than a half-written save.
		tmp := target + ".gs-tmp"
		out, err := os.Create(tmp)
		if err != nil {
			rc.Close()
			return nil, err
		}
		n, err := io.Copy(out, rc)
		rc.Close()
		closeErr := out.Close()
		if err != nil {
			os.Remove(tmp)
			return nil, fmt.Errorf("чтение %s: %w", f.Rel, err)
		}
		if closeErr != nil {
			os.Remove(tmp)
			return nil, closeErr
		}
		if err := os.Rename(tmp, target); err != nil {
			os.Remove(tmp)
			return nil, err
		}
		res.Files++
		res.TotalBytes += n
	}
	return res, nil
}

func withinDir(dir, target string) bool {
	absDir, err1 := filepath.Abs(dir)
	absTarget, err2 := filepath.Abs(target)
	if err1 != nil || err2 != nil {
		return false
	}
	return absTarget == absDir || strings.HasPrefix(absTarget, absDir+string(filepath.Separator))
}

// ─── Windows → Switch ──────────────────────────────────────────────────────

// PlannedWrite is one file a PC→Switch transfer would push.
type PlannedWrite struct {
	Rel        string `json:"rel"`
	Size       int64  `json:"size"`
	Replaces   bool   `json:"replaces"`
	replacesID string // device object id to delete first; empty for new files
	localPath  string
}

// UploadPlan is the complete, inspectable description of what a PC→Switch
// transfer would do to the console.
//
// Building the plan is strictly read-only, so it can be shown to the user (and
// tested) without touching the device. Nothing is written until ApplyUpload is
// called with the plan.
type UploadPlan struct {
	Title      string         `json:"title"`
	Profile    string         `json:"profile"`
	Writes     []PlannedWrite `json:"writes"`
	NewFiles   int            `json:"newFiles"`
	Replaced   int            `json:"replaced"`
	TotalBytes int64          `json:"totalBytes"`
	// Untouched lists files already on the console that the transfer will
	// leave exactly as they are. Shown to the user because "what survives"
	// matters as much as "what changes" when overwriting save data.
	Untouched []string `json:"untouched"`
	entryID   string
}

// PlanUpload compares a local save folder against a console profile and
// returns what would change. Read-only.
func PlanUpload(s *switchmtp.Session, srcDir, title, profile string) (*UploadPlan, error) {
	entry, err := FindEntry(s, title, profile)
	if err != nil {
		return nil, err
	}
	onDevice, err := s.Walk(entry.ID)
	if err != nil {
		return nil, err
	}
	deviceByRel := make(map[string]switchmtp.WalkEntry, len(onDevice))
	for _, f := range onDevice {
		deviceByRel[strings.ToLower(f.Rel)] = f
	}

	local, err := walkLocal(srcDir)
	if err != nil {
		return nil, err
	}
	if len(local) == 0 {
		return nil, fmt.Errorf("в папке %s нет файлов", srcDir)
	}

	plan := &UploadPlan{Title: title, Profile: profile, entryID: entry.ID}
	pushed := map[string]bool{}
	for _, lf := range local {
		w := PlannedWrite{Rel: lf.rel, Size: lf.size, localPath: lf.full}
		if d, ok := deviceByRel[strings.ToLower(lf.rel)]; ok {
			w.Replaces = true
			w.replacesID = d.ID
			plan.Replaced++
		} else {
			plan.NewFiles++
		}
		pushed[strings.ToLower(lf.rel)] = true
		plan.TotalBytes += lf.size
		plan.Writes = append(plan.Writes, w)
	}
	for _, f := range onDevice {
		if !pushed[strings.ToLower(f.Rel)] {
			plan.Untouched = append(plan.Untouched, f.Rel)
		}
	}
	sort.Slice(plan.Writes, func(i, j int) bool { return plan.Writes[i].Rel < plan.Writes[j].Rel })
	sort.Strings(plan.Untouched)
	return plan, nil
}

type localFile struct {
	rel  string
	full string
	size int64
}

func walkLocal(root string) ([]localFile, error) {
	var out []localFile
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return nil
		}
		out = append(out, localFile{rel: filepath.ToSlash(rel), full: p, size: info.Size()})
		return nil
	})
	return out, err
}

// Upload pushes a local save folder onto the console.
//
// 🔴 This is the only code in the app that modifies the Switch. It requires a
// session opened with writable=true, and callers must have taken a backup of
// the target profile first — Upload does not and cannot verify that, so the
// guarantee lives in app.go, where the backup runs unconditionally before this
// is reached.
//
// The plan is deliberately recomputed here instead of being handed in from the
// earlier preview: MTP object ids are only meaningful within the session that
// enumerated them, so replaying a plan built in a previous session could
// delete the wrong object. The preview is for the user to look at; this is the
// one that runs.
//
// Each file is deleted then re-created rather than written over, because MTP
// has no truncate: writing to an existing name yields a second object with a
// duplicate name instead of replacing the first.
func Upload(s *switchmtp.Session, srcDir, title, profile string, onProgress func(Progress)) (*UploadPlan, error) {
	plan, err := PlanUpload(s, srcDir, title, profile)
	if err != nil {
		return nil, err
	}
	if err := applyPlan(s, plan, onProgress); err != nil {
		return plan, err
	}
	return plan, nil
}

func applyPlan(s *switchmtp.Session, plan *UploadPlan, onProgress func(Progress)) error {
	if plan == nil || plan.entryID == "" {
		return fmt.Errorf("некорректный план переноса")
	}
	dirs := map[string]string{"": plan.entryID}
	for i, w := range plan.Writes {
		if onProgress != nil {
			onProgress(Progress{Done: i, Total: len(plan.Writes), Title: w.Rel})
		}
		parentID, err := ensureDir(s, dirs, plan.entryID, path.Dir(w.Rel))
		if err != nil {
			return err
		}
		if w.replacesID != "" {
			if err := s.Delete(w.replacesID); err != nil {
				return fmt.Errorf("удаление старого %s: %w", w.Rel, err)
			}
		}
		f, err := os.Open(w.localPath)
		if err != nil {
			return err
		}
		_, err = s.CreateFile(parentID, path.Base(w.Rel), w.Size, f)
		f.Close()
		if err != nil {
			return err
		}
	}
	if onProgress != nil {
		onProgress(Progress{Done: len(plan.Writes), Total: len(plan.Writes)})
	}
	return nil
}

// ensureDir resolves (creating if needed) a slash-separated relative directory
// under the profile root, memoising ids so a deep tree doesn't re-enumerate.
func ensureDir(s *switchmtp.Session, cache map[string]string, rootID, rel string) (string, error) {
	if rel == "." || rel == "/" || rel == "" {
		return rootID, nil
	}
	if id, ok := cache[rel]; ok {
		return id, nil
	}
	parentRel := path.Dir(rel)
	parentID, err := ensureDir(s, cache, rootID, parentRel)
	if err != nil {
		return "", err
	}
	name := path.Base(rel)
	if child, ok, err := s.Child(parentID, name); err != nil {
		return "", err
	} else if ok {
		cache[rel] = child.ID
		return child.ID, nil
	}
	id, err := s.CreateFolder(parentID, name)
	if err != nil {
		return "", fmt.Errorf("создание папки %s: %w", rel, err)
	}
	cache[rel] = id
	return id, nil
}
