//go:build windows

// Package switchsaves turns the raw MTP object tree exposed by the DBI
// responder into something the app can reason about, and snapshots it into the
// same zip + sidecar-manifest format the Windows backup engine uses.
//
// Layout DBI presents under the "Saves" store:
//
//	Saves/
//	  Installed games/<Title>/<Profile or save-data type>/<files…>
//	  Uninstalled games/<Title>/…
//
// The second level is the **console user profile** ("user", "SkywalkerC", …),
// not a save-data kind — 26 of the 74 titles on the reference console carry
// saves for two different profiles, and Minecraft for three. Backing a title
// up without splitting by profile would merge two people's saves into one
// archive, so Profile is part of the backup identity everywhere below.
// BCAT / Device / SD_Cache.* are the genuine save-data types and sit at the
// same level; they're classified separately so the UI can de-emphasise them.
package switchsaves

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"GameSaver/internal/domain"
	"GameSaver/internal/switchmtp"
	"GameSaver/internal/util"
)

// Store names as DBI publishes them; matched case-insensitively by substring
// because the store carries an index prefix ("7: Saves").
const (
	savesStoreHint = "saves"
	groupInstalled = "Installed games"
	groupUninstall = "Uninstalled games"
)

// EntryKind classifies a second-level folder under a title.
type EntryKind string

const (
	EntryProfile EntryKind = "profile" // a console user account — the common case
	EntryBCAT    EntryKind = "bcat"
	EntryDevice  EntryKind = "device"
	EntryCache   EntryKind = "cache"
)

// Entry is one profile's (or save-data type's) save tree for one title.
type Entry struct {
	Name      string    `json:"name"`
	ObjectID  string    `json:"objectId"`
	Kind      EntryKind `json:"kind"`
	FileCount int       `json:"fileCount"`
	SizeBytes int64     `json:"sizeBytes"`
}

// Title is one game as DBI names it (human readable, with ®/™ and unicode).
type Title struct {
	Name      string  `json:"name"`
	ObjectID  string  `json:"objectId"`
	Installed bool    `json:"installed"`
	Entries   []Entry `json:"entries"`
}

// Library is a full scan of the console's Saves store.
type Library struct {
	Device    switchmtp.Device `json:"device"`
	Titles    []Title          `json:"titles"`
	ScannedAt int64            `json:"scannedAt"`
}

// Progress is emitted while scanning so the UI can show something during the
// several-second walk.
type Progress struct {
	Done  int    `json:"done"`
	Total int    `json:"total"`
	Title string `json:"title"`
}

// classify maps a second-level folder name to its kind. Anything that isn't a
// known save-data type is a user profile — DBI names those after the console
// account nickname, so we cannot whitelist them.
func classify(name string) EntryKind {
	switch {
	case strings.EqualFold(name, "BCAT"):
		return EntryBCAT
	case strings.EqualFold(name, "Device"):
		return EntryDevice
	case strings.HasPrefix(strings.ToUpper(name), "SD_CACHE"):
		return EntryCache
	default:
		return EntryProfile
	}
}

// findSavesStore locates the "N: Saves" store among the device's storages.
func findSavesStore(s *switchmtp.Session) (switchmtp.Object, error) {
	stores, err := s.Children(s.RootID())
	if err != nil {
		return switchmtp.Object{}, fmt.Errorf("список разделов: %w", err)
	}
	for _, st := range stores {
		if strings.Contains(strings.ToLower(st.Name), savesStoreHint) {
			return st, nil
		}
	}
	return switchmtp.Object{}, fmt.Errorf("на устройстве нет раздела Saves (нужен MTP-режим DBI)")
}

// Scan reads the whole Saves store. measure=true additionally walks every
// profile to fill FileCount/SizeBytes, which costs a few seconds because it
// touches every object on the device.
func Scan(s *switchmtp.Session, dev switchmtp.Device, measure bool, onProgress func(Progress)) (*Library, error) {
	saves, err := findSavesStore(s)
	if err != nil {
		return nil, err
	}
	groups, err := s.Children(saves.ID)
	if err != nil {
		return nil, err
	}

	lib := &Library{Device: dev, ScannedAt: time.Now().Unix()}
	type pending struct {
		obj       switchmtp.Object
		installed bool
	}
	var todo []pending
	for _, g := range groups {
		installed := strings.EqualFold(g.Name, groupInstalled)
		if !installed && !strings.EqualFold(g.Name, groupUninstall) {
			continue
		}
		titles, err := s.Children(g.ID)
		if err != nil {
			return nil, err
		}
		for _, t := range titles {
			todo = append(todo, pending{obj: t, installed: installed})
		}
	}

	for i, p := range todo {
		if onProgress != nil {
			onProgress(Progress{Done: i, Total: len(todo), Title: p.obj.Name})
		}
		kids, err := s.Children(p.obj.ID)
		if err != nil {
			return nil, err
		}
		t := Title{Name: p.obj.Name, ObjectID: p.obj.ID, Installed: p.installed}
		for _, k := range kids {
			if !k.IsDir {
				continue
			}
			e := Entry{Name: k.Name, ObjectID: k.ID, Kind: classify(k.Name)}
			if measure {
				files, err := s.Walk(k.ID)
				if err != nil {
					return nil, err
				}
				e.FileCount = len(files)
				for _, f := range files {
					e.SizeBytes += f.Size
				}
			}
			t.Entries = append(t.Entries, e)
		}
		lib.Titles = append(lib.Titles, t)
	}
	sort.Slice(lib.Titles, func(i, j int) bool {
		return strings.ToLower(lib.Titles[i].Name) < strings.ToLower(lib.Titles[j].Name)
	})
	if onProgress != nil {
		onProgress(Progress{Done: len(todo), Total: len(todo)})
	}
	return lib, nil
}

// FindEntry locates a (title, profile) pair in a freshly-read tree. Object IDs
// are not stable across sessions, so callers pass names and we re-resolve.
func FindEntry(s *switchmtp.Session, titleName, entryName string) (switchmtp.Object, error) {
	saves, err := findSavesStore(s)
	if err != nil {
		return switchmtp.Object{}, err
	}
	groups, err := s.Children(saves.ID)
	if err != nil {
		return switchmtp.Object{}, err
	}
	for _, g := range groups {
		t, ok, err := s.Child(g.ID, titleName)
		if err != nil {
			return switchmtp.Object{}, err
		}
		if !ok {
			continue
		}
		e, ok, err := s.Child(t.ID, entryName)
		if err != nil {
			return switchmtp.Object{}, err
		}
		if ok {
			return e, nil
		}
	}
	return switchmtp.Object{}, fmt.Errorf("сейв не найден: %s / %s", titleName, entryName)
}

// ─── backup ────────────────────────────────────────────────────────────────

// SwitchDirName is the folder under BackupRoot that holds console backups.
//
// 🔴 The leading dot is load-bearing: backup.Reconcile skips top-level
// directories starting with "." when it rebuilds the DB from disk. Renaming
// this to something without the dot would make Reconcile import every Switch
// snapshot as a phantom Windows game with an unusable mtp:// save path.
const SwitchDirName = ".switch"

// BackupResult reports what one profile backup did.
type BackupResult struct {
	Title       string `json:"title"`
	Profile     string `json:"profile"`
	ArchivePath string `json:"archivePath"`
	FileCount   int    `json:"fileCount"`
	TotalBytes  int64  `json:"totalBytes"`
	Skipped     bool   `json:"skipped"` // identical content to the previous snapshot
	Error       string `json:"error,omitempty"`
}

// backupDir is <root>/.switch/<title-slug>/<profile-slug>.
func backupDir(root, title, profile string) string {
	return filepath.Join(root, SwitchDirName, util.Slug(title), util.Slug(profile))
}

// BackupEntry snapshots one (title, profile) into the backup root.
//
// Unlike the Windows engine, which stats then re-reads each file, this makes a
// single pass over the wire: every byte is streamed straight from MTP into the
// zip while being hashed. Re-reading would double the transfer time and, on a
// device the user might unplug, double the window for a partial read.
func BackupEntry(s *switchmtp.Session, dev switchmtp.Device, root, title, profile string, trigger domain.Trigger, appVersion string) (*BackupResult, error) {
	res := &BackupResult{Title: title, Profile: profile}

	entry, err := FindEntry(s, title, profile)
	if err != nil {
		return nil, err
	}
	files, err := s.Walk(entry.ID)
	if err != nil {
		return nil, fmt.Errorf("обход сейва: %w", err)
	}
	if len(files) == 0 {
		res.Skipped = true
		return res, nil
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Rel < files[j].Rel })

	dir := backupDir(root, title, profile)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	ts := time.Now()
	base := ts.UTC().Format("20060102T150405Z")
	tmpPath := filepath.Join(dir, base+".zip.tmp")

	manifestFiles, total, err := writeZipFromDevice(s, tmpPath, files, ts)
	if err != nil {
		os.Remove(tmpPath)
		return nil, err
	}
	contentHash := contentHashOf(manifestFiles)

	// Dedup against the newest existing snapshot for this exact (title,
	// profile). Done after the transfer because MTP gives us no mtime and no
	// per-object hash, so there is nothing cheaper to compare on.
	if prev, ok := latestHash(dir); ok && prev == contentHash {
		os.Remove(tmpPath)
		res.Skipped = true
		res.FileCount = len(manifestFiles)
		res.TotalBytes = total
		return res, nil
	}

	archivePath := filepath.Join(dir, base+"__"+contentHash[:8]+".zip")
	if err := os.Rename(tmpPath, archivePath); err != nil {
		os.Remove(tmpPath)
		return nil, err
	}
	m := domain.SnapshotManifest{
		GameName:      title,
		LocationPath:  DeviceURI(dev.Serial, title, profile),
		LocationKind:  domain.SaveKindCustom,
		CreatedAt:     ts.Unix(),
		Trigger:       trigger,
		Files:         manifestFiles,
		TotalBytes:    total,
		AppVersion:    appVersion,
		Platform:      domain.PlatformSwitch,
		SwitchTitle:   title,
		SwitchProfile: profile,
		SwitchSerial:  dev.Serial,
	}
	mb, _ := json.MarshalIndent(m, "", "  ")
	if err := os.WriteFile(strings.TrimSuffix(archivePath, ".zip")+".json", mb, 0o644); err != nil {
		return nil, fmt.Errorf("манифест: %w", err)
	}
	res.ArchivePath = archivePath
	res.FileCount = len(manifestFiles)
	res.TotalBytes = total
	return res, nil
}

// DeviceURI is the synthetic path recorded as a Switch backup's "location".
// It is deliberately not a filesystem path — nothing may try to os.Stat it.
func DeviceURI(serial, title, profile string) string {
	return "mtp://" + serial + "/Saves/" + title + "/" + profile
}

func writeZipFromDevice(s *switchmtp.Session, dest string, files []switchmtp.WalkEntry, ts time.Time) ([]domain.SnapshotFile, int64, error) {
	f, err := os.Create(dest)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()
	zw := zip.NewWriter(f)

	out := make([]domain.SnapshotFile, 0, len(files))
	var total int64
	for _, fe := range files {
		hdr := &zip.FileHeader{Name: fe.Rel, Method: zip.Deflate}
		// MTP/DBI reports no modification time for any object, so there is no
		// real timestamp to preserve. Stamp the snapshot time rather than let
		// the zero value land in 1979 and confuse extraction tools.
		hdr.Modified = ts
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			zw.Close()
			return nil, 0, err
		}
		rc, err := s.Open(fe.ID)
		if err != nil {
			zw.Close()
			return nil, 0, fmt.Errorf("чтение %s: %w", fe.Rel, err)
		}
		h := sha256.New()
		n, err := io.Copy(io.MultiWriter(w, h), rc)
		rc.Close()
		if err != nil {
			zw.Close()
			return nil, 0, fmt.Errorf("чтение %s: %w", fe.Rel, err)
		}
		out = append(out, domain.SnapshotFile{
			Rel:    fe.Rel,
			Size:   n,
			SHA256: hex.EncodeToString(h.Sum(nil)),
		})
		total += n
	}
	if err := zw.Close(); err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

// contentHashOf reproduces the Windows engine's scheme so both platforms'
// snapshots dedup identically: sha256 over sorted (rel, fileHash) pairs.
func contentHashOf(files []domain.SnapshotFile) string {
	cp := append([]domain.SnapshotFile(nil), files...)
	sort.Slice(cp, func(i, j int) bool { return cp[i].Rel < cp[j].Rel })
	h := sha256.New()
	for _, f := range cp {
		h.Write([]byte(f.Rel))
		h.Write([]byte{0})
		h.Write([]byte(f.SHA256))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func latestHash(dir string) (string, bool) {
	snaps := readManifests(dir)
	if len(snaps) == 0 {
		return "", false
	}
	return contentHashOf(snaps[0].Files), true
}

// ─── reading backups back off disk ─────────────────────────────────────────

// Backup is one console snapshot as listed for the UI.
type Backup struct {
	Title       string          `json:"title"`
	Profile     string          `json:"profile"`
	Serial      string          `json:"serial"`
	Platform    domain.Platform `json:"platform"`
	CreatedAt   int64           `json:"createdAt"`
	FileCount   int             `json:"fileCount"`
	TotalBytes  int64           `json:"totalBytes"`
	ArchivePath string          `json:"archivePath"`
	SizeOnDisk  int64           `json:"sizeOnDisk"`
	Trigger     domain.Trigger  `json:"trigger"`
}

// ListBackups reads every Switch snapshot manifest under the backup root.
// Disk is the source of truth here — there are no DB rows for these, which is
// deliberate: it keeps console snapshots out of the Windows game library and
// out of BackupAll/watcher, and means losing the DB loses nothing.
func ListBackups(root string) ([]Backup, error) {
	base := filepath.Join(root, SwitchDirName)
	var out []Backup
	titleDirs, err := os.ReadDir(base)
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return nil, err
	}
	for _, td := range titleDirs {
		if !td.IsDir() {
			continue
		}
		profDirs, err := os.ReadDir(filepath.Join(base, td.Name()))
		if err != nil {
			continue
		}
		for _, pd := range profDirs {
			if !pd.IsDir() {
				continue
			}
			dir := filepath.Join(base, td.Name(), pd.Name())
			for _, m := range readManifests(dir) {
				zipPath := strings.TrimSuffix(m.path, ".json") + ".zip"
				st, err := os.Stat(zipPath)
				if err != nil {
					continue // manifest without archive — nothing to restore
				}
				out = append(out, Backup{
					Title:       m.SwitchTitle,
					Profile:     m.SwitchProfile,
					Serial:      m.SwitchSerial,
					Platform:    m.PlatformOrDefault(),
					CreatedAt:   m.CreatedAt,
					FileCount:   len(m.Files),
					TotalBytes:  m.TotalBytes,
					ArchivePath: zipPath,
					SizeOnDisk:  st.Size(),
					Trigger:     m.Trigger,
				})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt > out[j].CreatedAt })
	return out, nil
}

type manifestOnDisk struct {
	domain.SnapshotManifest
	path string
}

// readManifests returns a directory's manifests, newest first.
func readManifests(dir string) []manifestOnDisk {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []manifestOnDisk
	for _, e := range ents {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".json") {
			continue
		}
		full := filepath.Join(dir, e.Name())
		raw, err := os.ReadFile(full)
		if err != nil {
			continue
		}
		var m domain.SnapshotManifest
		if err := json.Unmarshal(raw, &m); err != nil {
			continue
		}
		out = append(out, manifestOnDisk{SnapshotManifest: m, path: full})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt > out[j].CreatedAt })
	return out
}

// ApplyRetention prunes all but the newest keepN snapshots of one profile.
func ApplyRetention(root, title, profile string, keepN int) {
	if keepN <= 0 {
		return
	}
	dir := backupDir(root, title, profile)
	snaps := readManifests(dir)
	for i := keepN; i < len(snaps); i++ {
		os.Remove(snaps[i].path)
		os.Remove(strings.TrimSuffix(snaps[i].path, ".json") + ".zip")
	}
}
