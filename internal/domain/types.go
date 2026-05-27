package domain

// SourceKind identifies a launcher / install origin.
type SourceKind string

const (
	SourceSteam     SourceKind = "steam"
	SourceGOG       SourceKind = "gog"
	SourceEpic      SourceKind = "epic"
	SourceEA        SourceKind = "ea"
	SourceUbisoft   SourceKind = "ubisoft"
	SourceXbox      SourceKind = "xbox"
	SourceBattleNet SourceKind = "battlenet"
	SourceRiot      SourceKind = "riot"
	SourceMinecraft SourceKind = "minecraft"
	SourceMicrosoft SourceKind = "microsoftStore"
	SourceStandalone SourceKind = "standalone"
	SourcePirate    SourceKind = "pirate"
	SourceRepack    SourceKind = "repack"
	SourceUnknown   SourceKind = "unknown"
)

// SaveKind classifies where a save lives in the filesystem.
type SaveKind string

const (
	SaveKindSavedGames     SaveKind = "savedGames"
	SaveKindMyGames        SaveKind = "myGames"
	SaveKindDocuments      SaveKind = "documents"
	SaveKindAppDataLocal   SaveKind = "appDataLocal"
	SaveKindAppDataLow     SaveKind = "appDataLow"
	SaveKindAppDataRoaming SaveKind = "appDataRoaming"
	SaveKindInGameDir      SaveKind = "inGameDir"
	SaveKindGoldberg       SaveKind = "goldberg"
	SaveKindFLT            SaveKind = "flt"
	SaveKindRegistry       SaveKind = "registry"
	SaveKindCustom         SaveKind = "custom"
)

// Trigger reason for a backup snapshot.
type Trigger string

const (
	TriggerManual    Trigger = "manual"
	TriggerAuto      Trigger = "auto"
	TriggerPreRest   Trigger = "preRestore"
	TriggerPreMigrate Trigger = "preMigrate"
)

type Game struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Slug             string `json:"slug"`
	IGDBID           int64  `json:"igdbId,omitempty"`
	SteamAppID       int64  `json:"steamAppId,omitempty"`
	CoverPath        string `json:"coverPath,omitempty"`
	HeroPath         string `json:"heroPath,omitempty"`
	IconPath         string `json:"iconPath,omitempty"`
	Genres           string `json:"genres,omitempty"`
	ReleaseYear      int    `json:"releaseYear,omitempty"`
	Hidden           bool   `json:"hidden"`
	CreatedAt        int64  `json:"createdAt"`
	UpdatedAt        int64  `json:"updatedAt"`
	LastPlayedAt     int64  `json:"lastPlayedAt,omitempty"`
	TotalPlaySeconds int64  `json:"totalPlaySeconds,omitempty"`
}

// PlaySession is a single detected game run.
type PlaySession struct {
	ID              string `json:"id"`
	GameID          string `json:"gameId"`
	InstallationID  string `json:"installationId,omitempty"`
	StartedAt       int64  `json:"startedAt"`
	EndedAt         int64  `json:"endedAt,omitempty"`
	DurationSeconds int64  `json:"durationSeconds"`
	Source          string `json:"source"`
}

type Installation struct {
	ID         string     `json:"id"`
	GameID     string     `json:"gameId"`
	Source     SourceKind `json:"source"`
	SourceAppID string    `json:"sourceAppId,omitempty"`
	RootPath   string     `json:"rootPath"`
	ExePath    string     `json:"exePath"`
	SizeBytes  int64      `json:"sizeBytes,omitempty"`
	LastSeenAt int64      `json:"lastSeenAt"`
	LaunchURI  string     `json:"launchUri,omitempty"`
}

type SaveLocation struct {
	ID             string   `json:"id"`
	GameID         string   `json:"gameId"`
	Path           string   `json:"path"`
	Kind           SaveKind `json:"kind"`
	SourceHint     string   `json:"sourceHint,omitempty"`
	SizeBytes      int64    `json:"sizeBytes"`
	FileCount      int      `json:"fileCount"`
	Mtime          int64    `json:"mtime"`
	WatchEnabled   bool     `json:"watchEnabled"`
	IsJunction     bool     `json:"isJunction"`
	JunctionTarget string   `json:"junctionTarget,omitempty"`
}

type Snapshot struct {
	ID              string  `json:"id"`
	GameID          string  `json:"gameId"`
	LocationID      string  `json:"locationId"`
	ArchivePath     string  `json:"archivePath"`
	CreatedAt       int64   `json:"createdAt"`
	Trigger         Trigger `json:"trigger"`
	TotalBytes      int64   `json:"totalBytes"`
	CompressedBytes int64   `json:"compressedBytes"`
	FileCount       int     `json:"fileCount"`
	Encrypted       bool    `json:"encrypted"`
	ContentHash     string  `json:"contentHash"`
	GameVersion     string  `json:"gameVersion,omitempty"`
	Note            string  `json:"note,omitempty"`
}

// GameView is the aggregate sent to UI.
type GameView struct {
	Game          *Game           `json:"game"`
	Installations []*Installation `json:"installations"`
	SaveLocations []*SaveLocation `json:"saveLocations"`
	Snapshots     []*Snapshot     `json:"snapshots"`
}

// SnapshotManifest is what we write next to each .zip backup.
type SnapshotManifest struct {
	GameName     string         `json:"gameName"`
	GameID       string         `json:"gameId"`
	SteamAppID   int64          `json:"steamAppId,omitempty"`
	LocationPath string         `json:"locationPath"`
	LocationKind SaveKind       `json:"locationKind"`
	CreatedAt    int64          `json:"createdAt"`
	Trigger      Trigger        `json:"trigger"`
	Files        []SnapshotFile `json:"files"`
	TotalBytes   int64          `json:"totalBytes"`
	Encrypted    bool           `json:"encrypted"`
	AppVersion   string         `json:"appVersion"`
}

type SnapshotFile struct {
	Rel    string `json:"rel"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
	Mtime  int64  `json:"mtime"`
}
