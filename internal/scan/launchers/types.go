package launchers

import (
	"context"

	"GameSaver/internal/domain"
)

// ScanItem is the common output of any launcher scanner.
type ScanItem struct {
	Name        string
	Source      domain.SourceKind
	SourceAppID string
	SteamAppID  int64
	IGDBID      int64
	RootPath    string
	ExePath     string
	SizeBytes   int64
	LaunchURI   string
	InstallID   string
	GameKey     string // optional override for grouping; default is Slug(Name)
	LastSeenAt  int64
}

// Scanner is the per-source interface.
type Scanner interface {
	Name() string
	Scan(ctx context.Context) ([]*ScanItem, error)
}
