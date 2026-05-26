package util

import (
	"crypto/sha1"
	"encoding/hex"
	"regexp"
	"strings"
)

// Slug converts a free-form game name to a URL-safe slug.
func Slug(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = reInvalid.ReplaceAllString(s, "-")
	s = reDashes.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		s = "unknown"
	}
	return s
}

var (
	reInvalid = regexp.MustCompile(`[^a-z0-9]+`)
	reDashes  = regexp.MustCompile(`-+`)
)

// SHA1Hex returns the hex SHA1 of the input.
func SHA1Hex(s string) string {
	h := sha1.Sum([]byte(s))
	return hex.EncodeToString(h[:])
}

// InstallationID derives a stable ID from an absolute exe path.
func InstallationID(exePath string) string {
	return "inst_" + SHA1Hex(strings.ToLower(exePath))[:16]
}

// GameID derives a stable ID from a canonical slug and primary source app id (or root path).
func GameID(slug, primaryKey string) string {
	return "game_" + SHA1Hex(strings.ToLower(slug+"|"+primaryKey))[:16]
}

// SaveLocationID derives a stable ID from game id and save path.
func SaveLocationID(gameID, path string) string {
	return "sav_" + SHA1Hex(strings.ToLower(gameID+"|"+path))[:16]
}
