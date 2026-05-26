package match

import (
	"os"
	"path/filepath"
	"strings"
)

// TemplateVars is the set of substitutions for Ludusavi placeholders.
type TemplateVars struct {
	Base           string // game install root
	StoreUserID    string
	GameName       string
	WinAppData     string
	WinLocalAppData string
	WinLocalAppDataLow string
	WinDocuments   string
	WinPublic      string
	WinDir         string
	WinProgramData string
	Home           string
	Root           string // alias
	OS             string
}

func DefaultVars(gameName, baseDir string) TemplateVars {
	appData := os.Getenv("APPDATA")
	localApp := os.Getenv("LOCALAPPDATA")
	low := ""
	if localApp != "" {
		low = filepath.Join(filepath.Dir(localApp), "LocalLow")
	}
	home := os.Getenv("USERPROFILE")
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	docs := filepath.Join(home, "Documents")
	pub := filepath.Join(os.Getenv("PUBLIC"))
	if pub == "" {
		pub = `C:\Users\Public`
	}
	win := os.Getenv("SystemRoot")
	if win == "" {
		win = `C:\Windows`
	}
	progData := os.Getenv("PROGRAMDATA")
	if progData == "" {
		progData = `C:\ProgramData`
	}
	return TemplateVars{
		Base:               baseDir,
		GameName:           gameName,
		WinAppData:         appData,
		WinLocalAppData:    localApp,
		WinLocalAppDataLow: low,
		WinDocuments:       docs,
		WinPublic:          pub,
		WinDir:             win,
		WinProgramData:     progData,
		Home:               home,
		Root:               home,
		OS:                 "windows",
	}
}

var placeholders = map[string]func(v TemplateVars) string{
	"<base>":             func(v TemplateVars) string { return v.Base },
	"<game>":             func(v TemplateVars) string { return v.GameName },
	"<storeUserId>":      func(v TemplateVars) string { return v.StoreUserID },
	"<winAppData>":       func(v TemplateVars) string { return v.WinAppData },
	"<winLocalAppData>":  func(v TemplateVars) string { return v.WinLocalAppData },
	"<winLocalAppDataLow>": func(v TemplateVars) string { return v.WinLocalAppDataLow },
	"<winDocuments>":     func(v TemplateVars) string { return v.WinDocuments },
	"<winPublic>":        func(v TemplateVars) string { return v.WinPublic },
	"<winDir>":           func(v TemplateVars) string { return v.WinDir },
	"<winProgramData>":   func(v TemplateVars) string { return v.WinProgramData },
	"<home>":             func(v TemplateVars) string { return v.Home },
	"<root>":             func(v TemplateVars) string { return v.Root },
	"<osUserName>":       func(v TemplateVars) string { return os.Getenv("USERNAME") },
	"<os>":               func(v TemplateVars) string { return v.OS },
}

// Render substitutes placeholders in tmpl. Known placeholders use the resolved
// value from vars; if the resolved value is empty (e.g. <storeUserId> for which
// we have no context), the placeholder becomes "*" so Glob can enumerate the
// actual subfolders on disk. Unknown placeholders also become "*".
func Render(tmpl string, vars TemplateVars) string {
	s := tmpl
	for ph, fn := range placeholders {
		if strings.Contains(s, ph) {
			val := fn(vars)
			if val == "" {
				val = "*"
			}
			s = strings.ReplaceAll(s, ph, val)
		}
	}
	// Replace any remaining unknown <...> placeholder with "*" so the path
	// becomes a glob the caller can expand.
	s = unknownPlaceholderRe.ReplaceAllString(s, "*")
	// Normalize slashes to OS native (Glob characters * and ? pass through).
	s = strings.ReplaceAll(s, "/", string(filepath.Separator))
	return s
}

var unknownPlaceholderRe = mustCompileSimpleRegex()

func mustCompileSimpleRegex() *simpleRegex { return &simpleRegex{} }

// simpleRegex is a tiny shim: we don't want to pull in regexp just for one
// pattern. It replaces "<word>" tokens with "*".
type simpleRegex struct{}

func (r *simpleRegex) ReplaceAllString(s, repl string) string {
	out := s
	for {
		i := strings.Index(out, "<")
		if i < 0 {
			break
		}
		j := strings.Index(out[i:], ">")
		if j < 0 {
			break
		}
		out = out[:i] + repl + out[i+j+1:]
	}
	return out
}
