// Package tray drives the Windows system-tray icon for GameSaver. It runs on
// a background goroutine alongside the Wails GUI: when the user closes the
// window (HideWindowOnClose: true) the app keeps living here and the watcher
// keeps doing its job.
package tray

import (
	"context"
	"log/slog"

	"github.com/getlantern/systray"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// AppController is the slice of *App functionality the tray needs.
// Defined as an interface to keep this package import-free of `*main.App`.
type AppController interface {
	Context() context.Context
	BackupAll() (int, error)
	OpenBackupFolder() error
	WatcherToggle(enabled bool) error
	IsWatcherEnabled() bool
}

var (
	icon  []byte
	ctrl  AppController
	mPause *systray.MenuItem
)

// Init must be called once before Run. iconBytes should be a PNG or ICO
// (getlantern/systray accepts both on Windows).
func Init(iconBytes []byte, c AppController) {
	icon = iconBytes
	ctrl = c
}

// Run blocks until systray.Quit is called. Call from a goroutine before
// wails.Run; pair with Quit() during main shutdown.
func Run() {
	systray.Run(onReady, onExit)
}

// Quit asks systray to tear down. Safe to call when not running.
func Quit() {
	defer func() { _ = recover() }() // systray.Quit panics if not started
	systray.Quit()
}

func onReady() {
	systray.SetIcon(icon)
	systray.SetTitle("")
	systray.SetTooltip("GameSaver")

	mOpen := systray.AddMenuItem("Открыть GameSaver", "Показать окно прилы")
	systray.AddSeparator()
	mBackup := systray.AddMenuItem("Бэкап всех сейвов", "Снэпшот всех известных папок сейвов")
	mBackupFolder := systray.AddMenuItem("Открыть папку бэкапов", "Показать BackupRoot в Проводнике")
	systray.AddSeparator()
	mPause = systray.AddMenuItemCheckbox("Auto-backup (watcher)", "Включить/выключить слежение за сейвами", ctrl.IsWatcherEnabled())
	systray.AddSeparator()
	mQuit := systray.AddMenuItem("Выйти", "Закрыть GameSaver")

	go func() {
		for {
			select {
			case <-mOpen.ClickedCh:
				if ctx := ctrl.Context(); ctx != nil {
					wailsruntime.WindowShow(ctx)
					wailsruntime.WindowUnminimise(ctx)
				}
			case <-mBackup.ClickedCh:
				go func() {
					if n, err := ctrl.BackupAll(); err != nil {
						slog.Warn("tray: backup all", "err", err)
					} else {
						slog.Info("tray: backup all done", "snaps", n)
					}
				}()
			case <-mBackupFolder.ClickedCh:
				_ = ctrl.OpenBackupFolder()
			case <-mPause.ClickedCh:
				newState := !mPause.Checked()
				if err := ctrl.WatcherToggle(newState); err != nil {
					slog.Warn("tray: watcher toggle", "err", err)
					continue
				}
				if newState {
					mPause.Check()
				} else {
					mPause.Uncheck()
				}
			case <-mQuit.ClickedCh:
				if ctx := ctrl.Context(); ctx != nil {
					wailsruntime.Quit(ctx)
				}
				systray.Quit()
				return
			}
		}
	}()
}

func onExit() {
	slog.Info("tray exited")
}

// SyncWatcherState updates the tray checkbox after a programmatic toggle.
func SyncWatcherState(enabled bool) {
	if mPause == nil {
		return
	}
	if enabled {
		mPause.Check()
	} else {
		mPause.Uncheck()
	}
}
