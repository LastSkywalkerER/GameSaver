package stores

import (
	"strings"

	"golang.org/x/sys/windows/registry"

	"GameSaver/internal/domain"
)

// ClientStatus reports whether a store's desktop client (launcher) is installed
// on this PC, plus how to open it (its exe) or download it.
type ClientStatus struct {
	Store       domain.SourceKind `json:"store"`
	Installed   bool              `json:"installed"`
	ExePath     string            `json:"exePath"`     // main client exe (from the protocol handler) — launched on "Open"
	OpenURI     string            `json:"openUri"`     // protocol fallback if no exe
	DownloadURL string            `json:"downloadUrl"` // where to get it if not installed
}

// schemeCommand returns the registered launch command for a URL protocol, or "".
func schemeCommand(scheme string) string {
	k, err := registry.OpenKey(registry.CLASSES_ROOT, scheme+`\shell\open\command`, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer k.Close()
	v, _, err := k.GetStringValue("")
	if err != nil {
		return ""
	}
	return v
}

// exeFromCommand pulls the executable path out of a shell\open\command value
// like `"C:\…\Steam.exe" -- "%1"` (launching the exe bare opens the client).
func exeFromCommand(cmd string) string {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return ""
	}
	if cmd[0] == '"' {
		if end := strings.IndexByte(cmd[1:], '"'); end >= 0 {
			return cmd[1 : 1+end]
		}
	}
	if i := strings.IndexByte(cmd, ' '); i >= 0 {
		return cmd[:i]
	}
	return cmd
}

// DetectClients checks each store's launcher via its registered URL protocol and
// extracts the client exe so we can open it reliably.
func DetectClients() []ClientStatus {
	defs := []struct {
		store    domain.SourceKind
		schemes  []string
		openURI  string
		download string
	}{
		{domain.SourceSteam, []string{"steam"}, "steam://open/main", "https://store.steampowered.com/about/"},
		{domain.SourceGOG, []string{"goggalaxy"}, "goggalaxy://", "https://www.gog.com/galaxy"},
		{domain.SourceEpic, []string{"com.epicgames.launcher"}, "com.epicgames.launcher://", "https://store.epicgames.com/download"},
		{domain.SourceEA, []string{"link2ea", "origin2", "origin"}, "", "https://www.ea.com/ea-app"},
	}
	out := make([]ClientStatus, 0, len(defs))
	for _, d := range defs {
		st := ClientStatus{Store: d.store, OpenURI: d.openURI, DownloadURL: d.download}
		for _, s := range d.schemes {
			cmd := schemeCommand(s)
			if cmd == "" {
				continue
			}
			st.Installed = true
			st.ExePath = exeFromCommand(cmd)
			if st.OpenURI == "" {
				st.OpenURI = s + "://"
			}
			break
		}
		out = append(out, st)
	}
	return out
}
