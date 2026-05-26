package match

import (
	"fmt"
	"path/filepath"
	"strings"

	"GameSaver/internal/util"
)

// knownHubSuffixes are path FRAGMENTS that mark the boundary between OS /
// library shared dirs and game-specific namespace dirs. We match against the
// FULL path (case-insensitively) so a generic folder named "local" deep inside
// a game's save tree isn't confused with the "AppData\Local" hub.
// Order matters: longer / more specific entries first so the longest-match
// wins (e.g. "\AppData\Local\LocalLow" preferred over "\AppData\Local").
var knownHubSuffixes = []string{
	`\AppData\Local\LocalLow`,
	`\AppData\LocalLow`,
	`\AppData\Local`,
	`\AppData\Roaming`,
	`\Saved Games`,
	`\Documents\My Games`,
	`\Documents`,
	`\Public`,
	`\steamapps\common`,
	`\SteamLibrary\steamapps\common`,
	`\GOG Galaxy\Games`,
	`\GOG Games`,
	`\Epic Games`,
	`\Origin Games`,
	`\Ubisoft Games`,
	`\Battle.net`,
	`\XboxGames`,
	`\Games`,
}

// namespaceDir returns the deepest path prefix that names a single game's
// save / install dir, by finding the longest known shared-hub prefix in the
// path and taking the first segment that follows it.
//
//   C:\Users\X\AppData\Local\BendGame\Saved\Config
//   └─────── hub: \AppData\Local ───────┘ └namespace = …\BendGame
//
//   C:\Users\X\Saved Games\Respawn\Apex\local\settings.cfg
//   └────── hub: \Saved Games ────┘ └namespace = …\Saved Games\Respawn
//
// Returns "" if no hub prefix is found.
func namespaceDir(p string) string {
	clean := filepath.Clean(p)
	low := strings.ToLower(clean)
	bestEnd := -1
	for _, suf := range knownHubSuffixes {
		sufLow := strings.ToLower(suf)
		// scan all occurrences; pick the LAST (deepest) one that lands on a
		// path boundary and has a segment following it.
		for off := 0; ; {
			idx := strings.Index(low[off:], sufLow)
			if idx < 0 {
				break
			}
			abs := off + idx
			off = abs + 1
			// Verify the match starts at a path boundary (or at index 0).
			if abs > 0 && low[abs-1] != '\\' && low[abs-1] != '/' && low[abs] != '\\' && low[abs] != '/' {
				continue
			}
			end := abs + len(sufLow)
			// Verify the match ENDS at a path boundary (next char is separator
			// or end-of-string).
			if end < len(low) && low[end] != '\\' && low[end] != '/' {
				continue
			}
			if end > bestEnd {
				bestEnd = end
			}
		}
	}
	if bestEnd < 0 {
		return ""
	}
	// Take the first segment after the hub.
	rest := clean[bestEnd:]
	rest = strings.TrimLeft(rest, `\/`)
	if rest == "" {
		return ""
	}
	if i := strings.IndexAny(rest, `\/`); i >= 0 {
		rest = rest[:i]
	}
	return clean[:bestEnd] + `\` + rest
}

// commonParentSegments returns the longest common path prefix of all paths
// as a slice of segments. Returns empty slice if no common prefix.
func commonParentSegments(paths []string) []string {
	if len(paths) == 0 {
		return nil
	}
	base := splitPathSegments(filepath.Clean(paths[0]))
	for _, p := range paths[1:] {
		ps := splitPathSegments(filepath.Clean(p))
		n := 0
		for n < len(base) && n < len(ps) && strings.EqualFold(base[n], ps[n]) {
			n++
		}
		base = base[:n]
		if len(base) == 0 {
			return nil
		}
	}
	return base
}

func splitPathSegments(p string) []string {
	p = strings.ReplaceAll(p, "/", "\\")
	return strings.Split(p, "\\")
}

func joinSegments(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	if len(parts) == 1 {
		return parts[0]
	}
	if isDriveSegment(parts[0]) {
		return parts[0] + "\\" + strings.Join(parts[1:], "\\")
	}
	return strings.Join(parts, "\\")
}

func isDriveSegment(s string) bool {
	return len(s) == 2 && s[1] == ':'
}

// coalesceByNamespace groups candidates whose paths sit under the same game
// namespace dir and collapses each group into a single SaveLocation pointing
// at their deepest common ancestor.  Guarded against runaway coalescing into
// game install dirs: if the resulting parent contains more than 100 MB of
// data NOT included in the matched set, the coalesce is refused.
func coalesceByNamespace(cs []pathCand) []pathCand {
	type group struct {
		members []pathCand
	}
	groups := map[string]*group{}
	soloIdx := 0
	for _, c := range cs {
		ns := namespaceDir(c.path)
		if ns == "" {
			groups[fmt.Sprintf("__solo__%d", soloIdx)] = &group{members: []pathCand{c}}
			soloIdx++
			continue
		}
		key := strings.ToLower(ns)
		g, ok := groups[key]
		if !ok {
			g = &group{}
			groups[key] = g
		}
		g.members = append(g.members, c)
	}

	out := []pathCand{}
	for _, g := range groups {
		if len(g.members) <= 1 {
			out = append(out, g.members...)
			continue
		}
		paths := make([]string, len(g.members))
		for i, m := range g.members {
			paths[i] = m.path
		}
		segs := commonParentSegments(paths)
		if len(segs) == 0 {
			out = append(out, g.members...)
			continue
		}
		common := joinSegments(segs)
		st, err := osStat(common)
		if err != nil || !st.IsDir() {
			out = append(out, g.members...)
			continue
		}
		// Size guard: don't coalesce into a folder dominated by content we
		// didn't match (e.g. a 20 GB game install dir matched by a single
		// settings.ini).
		matchedBytes := int64(0)
		for _, m := range g.members {
			ms, err := osStat(m.path)
			if err != nil {
				continue
			}
			if ms.IsDir() {
				s, _, _ := util.DirSizeAndCount(m.path)
				matchedBytes += s
			} else {
				matchedBytes += ms.Size()
			}
		}
		parentBytes, _, _ := util.DirSizeAndCount(common)
		if parentBytes-matchedBytes > 100*1024*1024 && parentBytes > 3*matchedBytes {
			out = append(out, g.members...)
			continue
		}
		out = append(out, pathCand{path: common, isDir: true})
	}
	return out
}
