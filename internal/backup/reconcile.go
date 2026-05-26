package backup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"GameSaver/internal/domain"
	"GameSaver/internal/storage/sqlite"
	"GameSaver/internal/util"
)

// ReconcileResult summarises what Reconcile did.
type ReconcileResult struct {
	ScannedManifests int `json:"scannedManifests"`
	ImportedSnaps    int `json:"importedSnapshots"`
	AlreadyKnown     int `json:"alreadyKnown"`
	CreatedGames     int `json:"createdGames"`
	CreatedLocations int `json:"createdLocations"`
	OrphanZips       int `json:"orphanZips"`       // zip without sidecar manifest
	OrphanManifests  int `json:"orphanManifests"`  // manifest without zip
	DeadSnapshots    int `json:"deadSnapshots"`    // db rows whose archive file is gone
}

// Reconcile walks <BackupRoot>/<game>/*.json, parses sidecar manifests, and
// re-creates DB rows in `snapshots` (plus their `games` / `save_locations`
// parents if missing) for any archive present on disk but absent from DB.
// Also deletes DB snapshot rows whose archive file is no longer there.
//
// Safe to run repeatedly: every imported row is keyed off the same stable
// snapshot ID derived from (gameID, locationID, timestamp, contentHash) so
// re-imports are no-ops.
func (e *Engine) Reconcile(ctx context.Context) (*ReconcileResult, error) {
	res := &ReconcileResult{}
	root := e.cfg.BackupRoot
	if root == "" || !util.IsDir(root) {
		return res, nil
	}

	// Index all .zip files we see to later detect orphans (zip-only, no manifest).
	zipsByPath := map[string]bool{}
	manifestPaths := []string{}

	gameDirs, err := os.ReadDir(root)
	if err != nil {
		return res, err
	}
	for _, gd := range gameDirs {
		if !gd.IsDir() {
			continue
		}
		// Skip our own bookkeeping folder (.gamesaver/).
		if strings.HasPrefix(gd.Name(), ".") {
			continue
		}
		gameDir := filepath.Join(root, gd.Name())
		entries, err := os.ReadDir(gameDir)
		if err != nil {
			continue
		}
		for _, en := range entries {
			if en.IsDir() {
				continue
			}
			full := filepath.Join(gameDir, en.Name())
			switch strings.ToLower(filepath.Ext(en.Name())) {
			case ".zip":
				zipsByPath[strings.ToLower(full)] = true
			case ".json":
				manifestPaths = append(manifestPaths, full)
			}
		}
	}

	for _, mfPath := range manifestPaths {
		if ctx.Err() != nil {
			break
		}
		res.ScannedManifests++

		raw, err := os.ReadFile(mfPath)
		if err != nil {
			continue
		}
		var m domain.SnapshotManifest
		if err := json.Unmarshal(raw, &m); err != nil {
			slog.Warn("reconcile: bad manifest", "path", mfPath, "err", err)
			continue
		}
		// Locate the sibling zip (same basename, .zip extension).
		zipPath := strings.TrimSuffix(mfPath, ".json") + ".zip"
		zst, err := os.Stat(zipPath)
		if err != nil {
			res.OrphanManifests++
			continue
		}
		// Account for the zip in our index so we know it had a sidecar.
		delete(zipsByPath, strings.ToLower(zipPath))

		// Find or create the game.
		g, created, err := e.findOrCreateGame(&m)
		if err != nil {
			slog.Warn("reconcile: resolve game", "name", m.GameName, "err", err)
			continue
		}
		if created {
			res.CreatedGames++
		}

		// Find or create the save_location.
		loc, locCreated, err := e.findOrCreateLocation(g, &m)
		if err != nil {
			slog.Warn("reconcile: resolve location", "game", g.Name, "err", err)
			continue
		}
		if locCreated {
			res.CreatedLocations++
		}

		// Build a stable snapshot row.
		contentHash := computeContentHashFromManifest(m.Files)
		ts := time.Unix(m.CreatedAt, 0).UTC().Format("20060102T150405Z")
		sn := &domain.Snapshot{
			ID:              snapshotID(g.ID, loc.ID, ts, contentHash),
			GameID:          g.ID,
			LocationID:      loc.ID,
			ArchivePath:     zipPath,
			CreatedAt:       m.CreatedAt,
			Trigger:         m.Trigger,
			TotalBytes:      m.TotalBytes,
			CompressedBytes: zst.Size(),
			FileCount:       len(m.Files),
			Encrypted:       m.Encrypted,
			ContentHash:     contentHash,
		}
		// De-dupe: if row with this ID already there, skip.
		if existing, err := e.db.GetSnapshot(sn.ID); err == nil && existing != nil {
			res.AlreadyKnown++
			continue
		}
		if err := e.db.InsertSnapshot(sn); err != nil {
			slog.Warn("reconcile: insert snapshot", "id", sn.ID, "err", err)
			continue
		}
		res.ImportedSnaps++
	}

	// Anything left in zipsByPath is a zip with no sidecar manifest.
	res.OrphanZips = len(zipsByPath)

	// Sweep dead snapshots: db rows whose archive file is gone.
	dead, err := e.deadSnapshots()
	if err == nil {
		for _, sn := range dead {
			if err := e.db.DeleteSnapshot(sn.ID); err == nil {
				res.DeadSnapshots++
			}
		}
	}
	return res, nil
}

// findOrCreateGame resolves a manifest to a games row. Lookup order:
//   1. exact game id from manifest (if our current DB still has it),
//   2. Steam AppID,
//   3. slug of game name,
//   4. otherwise create a new stub game.
func (e *Engine) findOrCreateGame(m *domain.SnapshotManifest) (*domain.Game, bool, error) {
	if m.GameID != "" {
		if g, err := e.db.GetGame(m.GameID); err == nil {
			return g, false, nil
		}
	}
	if m.SteamAppID > 0 {
		if g, err := e.db.FindGameBySteamAppID(m.SteamAppID); err == nil {
			return g, false, nil
		}
	}
	slug := util.Slug(m.GameName)
	if g, err := e.db.FindGameBySlug(slug); err == nil {
		return g, false, nil
	}
	g := &domain.Game{
		ID:         m.GameID,
		Name:       m.GameName,
		Slug:       slug,
		SteamAppID: m.SteamAppID,
	}
	if g.ID == "" {
		g.ID = util.GameID(slug, fmt.Sprintf("reconcile:%s", m.GameName))
	}
	if err := e.db.UpsertGame(g); err != nil {
		return nil, false, err
	}
	return g, true, nil
}

// findOrCreateLocation returns the save_location row for this manifest path
// under the game, creating an entry if necessary.
func (e *Engine) findOrCreateLocation(g *domain.Game, m *domain.SnapshotManifest) (*domain.SaveLocation, bool, error) {
	id := util.SaveLocationID(g.ID, m.LocationPath)
	if existing, err := e.db.GetSaveLocation(id); err == nil {
		return existing, false, nil
	}
	kind := m.LocationKind
	if kind == "" {
		kind = domain.SaveKindCustom
	}
	loc := &domain.SaveLocation{
		ID:           id,
		GameID:       g.ID,
		Path:         m.LocationPath,
		Kind:         kind,
		SourceHint:   "reconciled",
		SizeBytes:    m.TotalBytes,
		FileCount:    len(m.Files),
		Mtime:        m.CreatedAt,
		WatchEnabled: false, // reconciled paths may not exist anymore
	}
	if err := e.db.UpsertSaveLocation(loc); err != nil {
		return nil, false, err
	}
	return loc, true, nil
}

func (e *Engine) deadSnapshots() ([]*domain.Snapshot, error) {
	games, err := e.db.ListGames()
	if err != nil {
		return nil, err
	}
	dead := []*domain.Snapshot{}
	for _, g := range games {
		snaps, _ := e.db.ListSnapshots(g.ID)
		for _, sn := range snaps {
			if _, err := os.Stat(sn.ArchivePath); err != nil {
				dead = append(dead, sn)
			}
		}
	}
	return dead, nil
}

// computeContentHashFromManifest reproduces engine.go's hashing scheme so
// reconciled snapshots get the same content_hash they'd have if backup_engine
// had just written them. (Sorted files → sha256(rel || 0 || file_sha256 || 0)*).
func computeContentHashFromManifest(files []domain.SnapshotFile) string {
	cp := make([]domain.SnapshotFile, len(files))
	copy(cp, files)
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

func snapshotID(gameID, locationID, ts, contentHash string) string {
	return "snap_" + util.SHA1Hex(gameID+"|"+locationID+"|"+ts+"|"+contentHash)[:16]
}

// Ensure unused import doesn't break if we later prune.
var _ = sqlite.ErrNotFound
