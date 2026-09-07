//go:build windows

package main

import (
	"errors"
	"fmt"
	"log/slog"
	"time"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"GameSaver/internal/domain"
	"GameSaver/internal/switchmtp"
	"GameSaver/internal/switchsaves"
	"GameSaver/internal/util"
)

// ─── connection state ──────────────────────────────────────────────────────

// SwitchStatus reports whether a console is attached over MTP right now.
type SwitchStatus struct {
	Connected bool   `json:"connected"`
	Name      string `json:"name"`
	Serial    string `json:"serial"`
	Error     string `json:"error,omitempty"`
}

// GetSwitchStatus is the pull-side counterpart to the switch:status event.
// Wails never replays events, so a tab that mounts after the poller has
// already fired would otherwise never learn the console is plugged in.
func (a *App) GetSwitchStatus() *SwitchStatus {
	dev, ok, err := switchmtp.FindSwitch()
	if err != nil {
		return &SwitchStatus{Error: err.Error()}
	}
	if !ok {
		return &SwitchStatus{}
	}
	return &SwitchStatus{Connected: true, Name: dev.FriendlyName, Serial: dev.Serial}
}

// watchSwitch polls for the console appearing/disappearing and emits
// switch:status on every change. Six seconds matches the playtime tracker's
// cadence — a USB device that was just plugged in should show up about as
// fast as a game launch does, and enumerating portable devices is cheap.
func (a *App) watchSwitch() {
	var last SwitchStatus
	for {
		cur := *a.GetSwitchStatus()
		if cur.Connected != last.Connected || cur.Serial != last.Serial {
			last = cur
			if a.ctx != nil {
				wailsruntime.EventsEmit(a.ctx, "switch:status", cur)
			}
			slog.Info("switch presence", "connected", cur.Connected, "serial", cur.Serial)
		}
		time.Sleep(6 * time.Second)
	}
}

// withSwitch runs fn against the attached console.
//
// writable=false asks the driver for GENERIC_READ only, so scanning and backup
// cannot alter save data no matter what the callback does. Only
// TransferPCToSwitch passes true.
func (a *App) withSwitch(writable bool, fn func(*switchmtp.Session, switchmtp.Device) error) error {
	dev, ok, err := switchmtp.FindSwitch()
	if err != nil {
		return fmt.Errorf("поиск устройства: %w", err)
	}
	if !ok {
		return errors.New("Switch не подключён по MTP (запусти MTP-режим в DBI)")
	}
	return switchmtp.WithSession(dev.PnPID, writable, func(s *switchmtp.Session) error {
		return fn(s, dev)
	})
}

func (a *App) emitSwitchProgress(phase string) func(switchsaves.Progress) {
	return func(p switchsaves.Progress) {
		if a.ctx == nil {
			return
		}
		wailsruntime.EventsEmit(a.ctx, "switch:progress", map[string]any{
			"phase": phase,
			"done":  p.Done,
			"total": p.Total,
			"title": p.Title,
		})
	}
}

// ─── scanning ──────────────────────────────────────────────────────────────

// ScanSwitch reads the console's whole Saves store, including per-profile file
// counts and sizes. Read-only.
func (a *App) ScanSwitch() (*switchsaves.Library, error) {
	var lib *switchsaves.Library
	err := a.withSwitch(false, func(s *switchmtp.Session, dev switchmtp.Device) error {
		l, err := switchsaves.Scan(s, dev, true, a.emitSwitchProgress("scan"))
		if err != nil {
			return err
		}
		lib = l
		return nil
	})
	if err != nil {
		return nil, err
	}
	return lib, nil
}

// ─── backup ────────────────────────────────────────────────────────────────

// SwitchSelection is one (title, profile) pair chosen in the UI. The profile is
// part of the identity because a title commonly holds saves for several
// console accounts.
type SwitchSelection struct {
	Title   string `json:"title"`
	Profile string `json:"profile"`
}

// BackupSwitchSelection snapshots every selected (title, profile) pair into
// <BackupRoot>\.switch\. Read-only with respect to the console.
func (a *App) BackupSwitchSelection(sel []SwitchSelection) ([]*switchsaves.BackupResult, error) {
	if len(sel) == 0 {
		return nil, errors.New("не выбрано ни одного сейва")
	}
	out := make([]*switchsaves.BackupResult, 0, len(sel))
	err := a.withSwitch(false, func(s *switchmtp.Session, dev switchmtp.Device) error {
		for i, sl := range sel {
			a.emitSwitchProgress("backup")(switchsaves.Progress{
				Done: i, Total: len(sel), Title: sl.Title + " / " + sl.Profile,
			})
			r, err := switchsaves.BackupEntry(s, dev, a.cfg.BackupRoot, sl.Title, sl.Profile,
				domain.TriggerManual, AppVersion)
			if err != nil {
				// One unreadable title shouldn't abort a 40-title batch.
				slog.Warn("switch backup", "title", sl.Title, "profile", sl.Profile, "err", err)
				out = append(out, &switchsaves.BackupResult{
					Title: sl.Title, Profile: sl.Profile, Error: err.Error(),
				})
				continue
			}
			switchsaves.ApplyRetention(a.cfg.BackupRoot, sl.Title, sl.Profile, a.cfg.RetentionKeepN)
			out = append(out, r)
		}
		a.emitSwitchProgress("backup")(switchsaves.Progress{Done: len(sel), Total: len(sel)})
		return nil
	})
	if err != nil {
		return out, err
	}
	return out, nil
}

// ListSwitchBackups reads console snapshots straight off disk. There are no DB
// rows for these on purpose: it keeps them out of the Windows game library,
// out of BackupAll and out of the fsnotify watcher, and means a lost DB costs
// nothing here.
func (a *App) ListSwitchBackups() ([]switchsaves.Backup, error) {
	return switchsaves.ListBackups(a.cfg.BackupRoot)
}

// ─── transfers ─────────────────────────────────────────────────────────────

// TransferSwitchToPC copies a console save into one of a PC game's save
// locations, after snapshotting that location so the overwrite is reversible.
func (a *App) TransferSwitchToPC(title, profile, locationID string) (*switchsaves.DownloadResult, error) {
	loc, err := a.db.GetSaveLocation(locationID)
	if err != nil {
		return nil, fmt.Errorf("папка сейва не найдена: %w", err)
	}
	// 🔴 Same rule as Restore: never overwrite a live save without capturing
	// the current state first.
	if _, err := a.bk.BackupGame(a.ctx, loc.GameID, domain.TriggerPreMigrate); err != nil {
		return nil, fmt.Errorf("не удалось сделать резервную копию перед переносом: %w", err)
	}
	var res *switchsaves.DownloadResult
	err = a.withSwitch(false, func(s *switchmtp.Session, _ switchmtp.Device) error {
		r, err := switchsaves.DownloadTo(s, title, profile, loc.Path)
		res = r
		return err
	})
	if err != nil {
		return nil, err
	}
	if size, count, mtime := util.DirSizeAndCount(loc.Path); count > 0 {
		loc.SizeBytes, loc.FileCount, loc.Mtime = size, count, mtime
		_ = a.db.UpsertSaveLocation(loc)
	}
	return res, nil
}

// GetSwitchTitleEntries lists the profiles that exist for one title on the
// console, so the restore dialog can offer real destinations. Read-only, and
// cheap enough to call when a dialog opens without a prior full scan.
func (a *App) GetSwitchTitleEntries(title string) ([]switchsaves.Entry, error) {
	var out []switchsaves.Entry
	err := a.withSwitch(false, func(s *switchmtp.Session, _ switchmtp.Device) error {
		e, err := switchsaves.TitleEntries(s, title)
		out = e
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ExtractSwitchBackupToPC unpacks a console backup into one of a PC game's
// save folders. The console is not touched at all; the PC side is snapshotted
// first because we're overwriting a live save.
func (a *App) ExtractSwitchBackupToPC(archivePath, locationID string) (*switchsaves.DownloadResult, error) {
	loc, err := a.db.GetSaveLocation(locationID)
	if err != nil {
		return nil, fmt.Errorf("папка сейва не найдена: %w", err)
	}
	if _, err := a.bk.BackupGame(a.ctx, loc.GameID, domain.TriggerPreMigrate); err != nil {
		return nil, fmt.Errorf("не удалось сделать резервную копию перед восстановлением: %w", err)
	}
	files, total, err := switchsaves.ExtractToDir(archivePath, loc.Path)
	if err != nil {
		return nil, err
	}
	if size, count, mtime := util.DirSizeAndCount(loc.Path); count > 0 {
		loc.SizeBytes, loc.FileCount, loc.Mtime = size, count, mtime
		_ = a.db.UpsertSaveLocation(loc)
	}
	return &switchsaves.DownloadResult{Files: files, TotalBytes: total}, nil
}

// RestoreSwitchBackupToConsole writes a console backup back onto the Switch,
// onto whichever profile the user picked (not necessarily the one it came
// from).
//
// 🔴 Writes to the console. The destination profile is backed up first and a
// failure there aborts the restore — see switchsaves.RestoreToConsole.
func (a *App) RestoreSwitchBackupToConsole(archivePath, title, profile string) (*switchsaves.UploadPlan, error) {
	var plan *switchsaves.UploadPlan
	err := a.withSwitch(true, func(s *switchmtp.Session, dev switchmtp.Device) error {
		p, err := switchsaves.RestoreToConsole(s, dev, a.cfg.BackupRoot, archivePath,
			title, profile, AppVersion, a.emitSwitchProgress("restore"))
		plan = p
		return err
	})
	if err != nil {
		return plan, err
	}
	slog.Info("switch restore done", "title", title, "profile", profile, "archive", archivePath)
	return plan, nil
}

// PreviewSwitchUpload shows what pushing a PC save onto the console would
// change, without touching the device. The session is read-only, so this is
// safe to call freely from the UI.
func (a *App) PreviewSwitchUpload(locationID, title, profile string) (*switchsaves.UploadPlan, error) {
	loc, err := a.db.GetSaveLocation(locationID)
	if err != nil {
		return nil, fmt.Errorf("папка сейва не найдена: %w", err)
	}
	var plan *switchsaves.UploadPlan
	err = a.withSwitch(false, func(s *switchmtp.Session, _ switchmtp.Device) error {
		p, err := switchsaves.PlanUpload(s, loc.Path, title, profile)
		plan = p
		return err
	})
	if err != nil {
		return nil, err
	}
	return plan, nil
}

// TransferPCToSwitch writes a PC save onto the console.
//
// 🔴 This is the only path in the app that modifies the Switch. The backup of
// the target profile below is unconditional and its failure aborts the
// transfer — if we cannot capture the console's current save, we do not touch
// it. Do not add a "skip backup" flag here.
func (a *App) TransferPCToSwitch(locationID, title, profile string) (*switchsaves.UploadPlan, error) {
	loc, err := a.db.GetSaveLocation(locationID)
	if err != nil {
		return nil, fmt.Errorf("папка сейва не найдена: %w", err)
	}
	var plan *switchsaves.UploadPlan
	err = a.withSwitch(true, func(s *switchmtp.Session, dev switchmtp.Device) error {
		a.emitSwitchProgress("preBackup")(switchsaves.Progress{Total: 1, Title: title + " / " + profile})
		bk, err := switchsaves.BackupEntry(s, dev, a.cfg.BackupRoot, title, profile,
			domain.TriggerPreRest, AppVersion)
		if err != nil {
			return fmt.Errorf("резервная копия сейва на консоли не удалась, перенос отменён: %w", err)
		}
		slog.Info("switch pre-transfer backup", "title", title, "profile", profile,
			"archive", bk.ArchivePath, "skipped", bk.Skipped)

		p, err := switchsaves.Upload(s, loc.Path, title, profile, a.emitSwitchProgress("upload"))
		plan = p
		return err
	})
	if err != nil {
		return plan, err
	}
	return plan, nil
}
