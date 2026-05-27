package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Config is the persisted application configuration.
type Config struct {
	UserDataDir       string   `json:"-"`
	BackupRoot        string   `json:"backupRoot"`
	SteamGridDBKey    string   `json:"steamGridDbKey,omitempty"`
	Language          string   `json:"language"`
	Theme             string   `json:"theme"`
	ExtraScanRoots    []string `json:"extraScanRoots,omitempty"`
	ExcludeScanRoots  []string `json:"excludeScanRoots,omitempty"`
	WatcherEnabled    bool     `json:"watcherEnabled"`
	WatcherDebounceMs int      `json:"watcherDebounceMs"`
	RetentionKeepN    int      `json:"retentionKeepN"`
	AutostartTray     bool     `json:"autostartTray"`

	// Auto-update preferences.
	AutoCheckUpdates    bool   `json:"autoCheckUpdates"`
	SkippedUpdateVer    string `json:"skippedUpdateVersion,omitempty"`
	LastUpdateCheckUnix int64  `json:"lastUpdateCheckUnix,omitempty"`
}

const (
	appName       = "GameSaver"
	defaultBackup = `C:\GameSavesBackup`
)

func defaultConfig() *Config {
	return &Config{
		BackupRoot: defaultBackup,
		// Empty language means "no user choice yet" — frontend picks from
		// navigator.language. Becomes "ru"/"en" the first time the user
		// touches the Language toggle in Settings.
		Language:          "",
		Theme:             "dark",
		WatcherEnabled:    false,
		WatcherDebounceMs: 10 * 60 * 1000,
		RetentionKeepN:    20,
		AutostartTray:     false,
		AutoCheckUpdates:  true,
	}
}

// SettingsPath returns the full path to settings.json under %APPDATA%.
func SettingsPath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "settings.json"), nil
}

// UserDataDir returns the per-user data directory for the app (under %LOCALAPPDATA%).
func UserDataDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, appName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

func configDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, appName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// Load reads settings.json or creates one with defaults.
func Load() (*Config, error) {
	path, err := SettingsPath()
	if err != nil {
		return nil, err
	}
	c := defaultConfig()
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, c)
	}
	userData, err := UserDataDir()
	if err != nil {
		return nil, err
	}
	c.UserDataDir = userData

	// ensure backup root exists
	_ = os.MkdirAll(c.BackupRoot, 0o755)
	// ensure log/cache dirs exist
	_ = os.MkdirAll(filepath.Join(userData, "logs"), 0o755)
	_ = os.MkdirAll(filepath.Join(userData, "cache", "covers"), 0o755)

	return c, nil
}

// Save persists settings.json atomically.
func Save(c *Config) error {
	path, err := SettingsPath()
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// DBPath returns the SQLite file path under user data dir.
func (c *Config) DBPath() string {
	return filepath.Join(c.UserDataDir, "gamesaver.db")
}

func (c *Config) CoversDir() string {
	return filepath.Join(c.UserDataDir, "cache", "covers")
}

func (c *Config) LogsDir() string {
	return filepath.Join(c.UserDataDir, "logs")
}
