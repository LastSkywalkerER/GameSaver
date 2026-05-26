package main

import (
	"context"
	"embed"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"

	"GameSaver/internal/config"
	"GameSaver/internal/logging"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("load config", "err", err)
		os.Exit(1)
	}
	if err := logging.Setup(cfg); err != nil {
		slog.Error("setup logging", "err", err)
		os.Exit(1)
	}
	slog.Info("GameSaver starting", "version", AppVersion, "userData", cfg.UserDataDir)

	app := NewApp(cfg)

	err = wails.Run(&options.App{
		Title:             "GameSaver",
		Width:             1280,
		Height:            820,
		MinWidth:          900,
		MinHeight:         600,
		BackgroundColour:  &options.RGBA{R: 17, G: 17, B: 23, A: 1},
		AssetServer: &assetserver.Options{
			Assets:  assets,
			Handler: coversHandler(cfg),
		},
		OnStartup:         func(ctx context.Context) { app.Startup(ctx) },
		OnShutdown:        func(ctx context.Context) { app.Shutdown(ctx) },
		HideWindowOnClose: false,
		Bind:              []interface{}{app},
		Windows: &windows.Options{
			WebviewIsTransparent:              false,
			WindowIsTranslucent:               false,
			DisableFramelessWindowDecorations: false,
		},
	})
	if err != nil {
		slog.Error("wails run", "err", err)
		os.Exit(1)
	}
}

// coversHandler returns an http.Handler that serves cached cover images.
// Requests to /covers/<filename> resolve to files under cfg.CoversDir().
// Anything else returns 404 so the embedded SPA index can take over.
func coversHandler(cfg *config.Config) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/covers/") {
			http.NotFound(w, r)
			return
		}
		name := strings.TrimPrefix(r.URL.Path, "/covers/")
		if name == "" || strings.Contains(name, "..") || strings.ContainsAny(name, `\/`) {
			http.NotFound(w, r)
			return
		}
		full := filepath.Join(cfg.CoversDir(), name)
		http.ServeFile(w, r, full)
	})
}

// AppVersion is overridden at build time via:
//
//	wails build -ldflags "-X main.AppVersion=v0.2.0"
//
// Local dev builds keep "dev" so the auto-updater skips the version check.
var AppVersion = "dev"
