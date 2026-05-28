package backup

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"GameSaver/internal/config"
	"GameSaver/internal/domain"
	"GameSaver/internal/storage/sqlite"
	"GameSaver/internal/util"
)

// Engine handles snapshot creation, restoration and retention.
type Engine struct {
	cfg *config.Config
	db  *sqlite.Store
	mu  sync.Mutex
}

func New(cfg *config.Config, db *sqlite.Store) *Engine {
	return &Engine{cfg: cfg, db: db}
}

// MigrateSave copies the contents of one of a game's save locations into
// another (e.g. pirate → Steam after switching versions). It first takes a
// preMigrate backup of the WHOLE game (so the destination is recoverable),
// then merges the source tree over the destination, then refreshes the
// destination location's size/count/mtime in the DB.
//
// Both locations must belong to the same game. Files present only in the
// destination are kept; same-named files are overwritten by the source.
func (e *Engine) MigrateSave(ctx context.Context, gameID, fromLocID, toLocID string) error {
	if fromLocID == toLocID {
		return errors.New("источник и назначение совпадают")
	}
	from, err := e.db.GetSaveLocation(fromLocID)
	if err != nil {
		return fmt.Errorf("источник: %w", err)
	}
	to, err := e.db.GetSaveLocation(toLocID)
	if err != nil {
		return fmt.Errorf("назначение: %w", err)
	}
	if from.GameID != gameID || to.GameID != gameID {
		return errors.New("локации принадлежат разным играм")
	}
	fst, err := os.Stat(from.Path)
	if err != nil {
		return fmt.Errorf("источник недоступен: %w", err)
	}

	// Safety net: snapshot everything (incl. destination) before we touch it.
	if _, err := e.BackupGame(ctx, gameID, domain.TriggerPreMigrate); err != nil {
		slog.Warn("preMigrate backup", "err", err)
	}

	if fst.IsDir() {
		if err := copyTree(from.Path, to.Path); err != nil {
			return fmt.Errorf("копирование: %w", err)
		}
	} else {
		dst := to.Path
		if di, err := os.Stat(to.Path); err == nil && di.IsDir() {
			dst = filepath.Join(to.Path, filepath.Base(from.Path))
		}
		if err := copyFile(from.Path, dst); err != nil {
			return fmt.Errorf("копирование файла: %w", err)
		}
	}

	if st, err := os.Stat(to.Path); err == nil {
		if st.IsDir() {
			to.SizeBytes, to.FileCount, to.Mtime = util.DirSizeAndCount(to.Path)
		} else {
			to.SizeBytes, to.FileCount, to.Mtime = st.Size(), 1, st.ModTime().Unix()
		}
		_ = e.db.UpsertSaveLocation(to)
	}
	return nil
}

// copyTree recursively merges src into dst (dst created if missing).
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return copyFile(p, target)
	})
}

// copyFile copies a single file's contents (creating/truncating dst).
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}

// BackupGame snapshots every SaveLocation of a game; returns the new snapshots
// (or skipped ones with hash equal to latest).
func (e *Engine) BackupGame(ctx context.Context, gameID string, trigger domain.Trigger) ([]*domain.Snapshot, error) {
	g, err := e.db.GetGame(gameID)
	if err != nil {
		return nil, err
	}
	locs, err := e.db.ListSaveLocations(gameID)
	if err != nil {
		return nil, err
	}
	out := []*domain.Snapshot{}
	for _, loc := range locs {
		if ctx.Err() != nil {
			break
		}
		if !util.IsDir(loc.Path) && !util.PathExists(loc.Path) {
			continue
		}
		sn, err := e.snapshotLocation(g, loc, trigger)
		if err != nil {
			slog.Warn("snapshot", "game", g.Name, "loc", loc.Path, "err", err)
			continue
		}
		if sn != nil {
			out = append(out, sn)
		}
	}
	return out, nil
}

// BackupAll runs BackupGame for every game with saves.
func (e *Engine) BackupAll(ctx context.Context, trigger domain.Trigger) (int, error) {
	games, err := e.db.ListGames()
	if err != nil {
		return 0, err
	}
	created := 0
	for _, g := range games {
		if ctx.Err() != nil {
			break
		}
		sns, err := e.BackupGame(ctx, g.ID, trigger)
		if err != nil {
			slog.Warn("backup all", "game", g.Name, "err", err)
			continue
		}
		created += len(sns)
	}
	return created, nil
}

func (e *Engine) snapshotLocation(g *domain.Game, loc *domain.SaveLocation, trigger domain.Trigger) (*domain.Snapshot, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Collect files and compute the content hash before any disk work.
	files, totalBytes, contentHash, err := scanFiles(loc.Path)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, nil
	}

	if prev, _ := e.db.LatestSnapshotHash(g.ID, loc.ID); prev == contentHash {
		slog.Info("snapshot skipped (no changes)", "game", g.Name, "loc", loc.Path)
		return nil, nil
	}

	ts := time.Now()
	folder := filepath.Join(e.cfg.BackupRoot, util.Slug(g.Name))
	if err := os.MkdirAll(folder, 0o755); err != nil {
		return nil, err
	}
	tsStr := ts.UTC().Format("20060102T150405Z")
	suffix := shortHash(loc.Path)
	archiveName := fmt.Sprintf("%s__%s.zip", tsStr, suffix)
	manifestName := fmt.Sprintf("%s__%s.json", tsStr, suffix)
	archivePath := filepath.Join(folder, archiveName)
	manifestPath := filepath.Join(folder, manifestName)

	// Write zip
	manifest := domain.SnapshotManifest{
		GameName:     g.Name,
		GameID:       g.ID,
		SteamAppID:   g.SteamAppID,
		LocationPath: loc.Path,
		LocationKind: loc.Kind,
		CreatedAt:    ts.Unix(),
		Trigger:      trigger,
		Files:        files,
		TotalBytes:   totalBytes,
		Encrypted:    false,
		AppVersion:   "0.1.0",
	}

	compressed, err := writeZip(archivePath, loc.Path, files)
	if err != nil {
		return nil, err
	}
	// Write manifest sidecar
	mb, _ := json.MarshalIndent(manifest, "", "  ")
	if err := os.WriteFile(manifestPath, mb, 0o644); err != nil {
		slog.Warn("write manifest", "err", err)
	}

	sn := &domain.Snapshot{
		ID:              fmt.Sprintf("snap_%s", util.SHA1Hex(g.ID+"|"+loc.ID+"|"+tsStr+"|"+contentHash)[:16]),
		GameID:          g.ID,
		LocationID:      loc.ID,
		ArchivePath:     archivePath,
		CreatedAt:       ts.Unix(),
		Trigger:         trigger,
		TotalBytes:      totalBytes,
		CompressedBytes: compressed,
		FileCount:       len(files),
		Encrypted:       false,
		ContentHash:     contentHash,
	}
	if err := e.db.InsertSnapshot(sn); err != nil {
		return nil, err
	}
	// Update the snapshot location stats so UI gets fresh size/mtime.
	if size, count, mtime := util.DirSizeAndCount(loc.Path); count > 0 {
		loc.SizeBytes = size
		loc.FileCount = count
		loc.Mtime = mtime
		_ = e.db.UpsertSaveLocation(loc)
	}
	// retention
	e.applyRetention(loc.ID)
	return sn, nil
}

func (e *Engine) applyRetention(locationID string) {
	keep := e.cfg.RetentionKeepN
	if keep <= 0 {
		return
	}
	total, err := e.db.CountSnapshots(locationID)
	if err != nil || total <= keep {
		return
	}
	excess := total - keep
	olds, err := e.db.OldestSnapshots(locationID, excess)
	if err != nil {
		return
	}
	for _, sn := range olds {
		_ = e.DeleteSnapshot(sn.ID)
	}
}

// DeleteSnapshot removes archive + manifest + db row.
func (e *Engine) DeleteSnapshot(id string) error {
	sn, err := e.db.GetSnapshot(id)
	if err != nil {
		return err
	}
	_ = os.Remove(sn.ArchivePath)
	_ = os.Remove(strings.TrimSuffix(sn.ArchivePath, ".zip") + ".json")
	return e.db.DeleteSnapshot(id)
}

// Restore replaces files in the original save location with the snapshot contents.
// If overwrite=false, files that already exist are kept.
func (e *Engine) Restore(ctx context.Context, snapshotID string, overwrite bool) error {
	sn, err := e.db.GetSnapshot(snapshotID)
	if err != nil {
		return err
	}
	loc, err := e.db.GetSaveLocation(sn.LocationID)
	if err != nil {
		return err
	}
	g, err := e.db.GetGame(sn.GameID)
	if err != nil {
		return err
	}
	// Safety snapshot first
	if util.IsDir(loc.Path) {
		_, _ = e.snapshotLocation(g, loc, domain.TriggerPreRest)
	}
	return unzipInto(sn.ArchivePath, loc.Path, overwrite)
}

// ---------- helpers ----------

func scanFiles(root string) ([]domain.SnapshotFile, int64, string, error) {
	rootInfo, err := os.Stat(root)
	if err != nil {
		return nil, 0, "", err
	}
	files := []domain.SnapshotFile{}
	var total int64
	hasher := sha256.New()
	if rootInfo.IsDir() {
		err = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return nil
			}
			rel, _ := filepath.Rel(root, p)
			fh, err := os.Open(p)
			if err != nil {
				return nil
			}
			h := sha256.New()
			if _, err := io.Copy(h, fh); err != nil {
				fh.Close()
				return nil
			}
			fh.Close()
			sum := hex.EncodeToString(h.Sum(nil))
			files = append(files, domain.SnapshotFile{Rel: filepath.ToSlash(rel), Size: info.Size(), SHA256: sum, Mtime: info.ModTime().Unix()})
			total += info.Size()
			return nil
		})
		if err != nil {
			return nil, 0, "", err
		}
	} else {
		fh, err := os.Open(root)
		if err != nil {
			return nil, 0, "", err
		}
		h := sha256.New()
		_, _ = io.Copy(h, fh)
		fh.Close()
		sum := hex.EncodeToString(h.Sum(nil))
		files = append(files, domain.SnapshotFile{Rel: filepath.Base(root), Size: rootInfo.Size(), SHA256: sum, Mtime: rootInfo.ModTime().Unix()})
		total = rootInfo.Size()
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Rel < files[j].Rel })
	for _, f := range files {
		hasher.Write([]byte(f.Rel))
		hasher.Write([]byte{0})
		hasher.Write([]byte(f.SHA256))
		hasher.Write([]byte{0})
	}
	return files, total, hex.EncodeToString(hasher.Sum(nil)), nil
}

func writeZip(archive, root string, files []domain.SnapshotFile) (int64, error) {
	f, err := os.Create(archive + ".tmp")
	if err != nil {
		return 0, err
	}
	zw := zip.NewWriter(f)
	rootInfo, _ := os.Stat(root)
	rootIsFile := rootInfo != nil && !rootInfo.IsDir()
	for _, fe := range files {
		src := root
		if !rootIsFile {
			src = filepath.Join(root, filepath.FromSlash(fe.Rel))
		}
		in, err := os.Open(src)
		if err != nil {
			continue
		}
		hdr := &zip.FileHeader{Name: fe.Rel, Method: zip.Deflate}
		hdr.Modified = time.Unix(fe.Mtime, 0)
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			in.Close()
			zw.Close()
			f.Close()
			os.Remove(archive + ".tmp")
			return 0, err
		}
		if _, err := io.Copy(w, in); err != nil {
			in.Close()
			zw.Close()
			f.Close()
			os.Remove(archive + ".tmp")
			return 0, err
		}
		in.Close()
	}
	if err := zw.Close(); err != nil {
		f.Close()
		os.Remove(archive + ".tmp")
		return 0, err
	}
	if err := f.Close(); err != nil {
		os.Remove(archive + ".tmp")
		return 0, err
	}
	if err := os.Rename(archive+".tmp", archive); err != nil {
		return 0, err
	}
	st, err := os.Stat(archive)
	if err != nil {
		return 0, err
	}
	return st.Size(), nil
}

func unzipInto(archive, dest string, overwrite bool) error {
	zr, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer zr.Close()
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	for _, f := range zr.File {
		// Reject zip-slip: ensure resolved path stays under dest.
		target := filepath.Join(dest, filepath.FromSlash(f.Name))
		absDest, _ := filepath.Abs(dest)
		absTarget, _ := filepath.Abs(target)
		if !strings.HasPrefix(absTarget, absDest+string(filepath.Separator)) && absTarget != absDest {
			return errors.New("zip-slip detected: " + f.Name)
		}
		if f.FileInfo().IsDir() {
			_ = os.MkdirAll(target, 0o755)
			continue
		}
		if _, err := os.Stat(target); err == nil && !overwrite {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		out, err := os.Create(target)
		if err != nil {
			return err
		}
		in, err := f.Open()
		if err != nil {
			out.Close()
			return err
		}
		if _, err := io.Copy(out, in); err != nil {
			out.Close()
			in.Close()
			return err
		}
		in.Close()
		out.Close()
		_ = os.Chtimes(target, time.Now(), f.Modified)
	}
	return nil
}

func shortHash(s string) string {
	return util.SHA1Hex(s)[:8]
}
