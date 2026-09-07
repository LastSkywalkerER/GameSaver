// Command gs-switch is a read-only harness for the WPD transport in
// internal/switchmtp. It exists because the WPD PROPERTYKEYs (unlike the
// CLSIDs and IIDs) cannot be verified against the registry — the only way to
// know they're right is to point them at a real device and see whether names
// and sizes come back.
//
// It never writes to the device.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"GameSaver/internal/switchmtp"
	"GameSaver/internal/switchsaves"
)

func main() {
	dump := flag.String("dump", "", "dump the save tree for this game title (substring match)")
	read := flag.String("read", "", "hash one file by its path under the Saves store, e.g. 'Mario Kart 8 Deluxe/user/userdata.dat'")
	scan := flag.Bool("scan", false, "run the full switchsaves.Scan with measurement and report timing")
	backup := flag.String("backup", "", "back up one title (exact DBI name)")
	profile := flag.String("profile", "user", "profile to use with -backup / -plan")
	root := flag.String("root", "", "backup root for -backup")
	plan := flag.String("plan", "", "dry-run a PC->Switch transfer from this local folder (read-only, writes nothing)")
	title := flag.String("title", "", "console title to compare against with -plan / -restore")
	hash := flag.String("hash", "", "hash every file of this title's chosen profile (read-only)")
	restore := flag.String("restore", "", "WRITES TO THE CONSOLE: restore this backup archive onto -title/-profile")
	upload := flag.String("upload", "", "WRITES TO THE CONSOLE: push this local folder onto -title/-profile (no pre-backup; harness only)")
	flag.Parse()

	devs, err := switchmtp.ListDevices()
	if err != nil {
		fail("list devices: %v", err)
	}
	fmt.Printf("portable devices: %d\n", len(devs))
	var sw switchmtp.Device
	found := false
	for _, d := range devs {
		mark := " "
		if d.IsSwitch() {
			mark = "*"
			if !found {
				sw, found = d, true
			}
		}
		fmt.Printf(" %s %-28s serial=%-20s %s\n", mark, d.FriendlyName, d.Serial, d.Description)
	}
	if !found {
		fail("no Nintendo device attached")
	}
	fmt.Printf("\nusing: %s (serial %s)\n", sw.FriendlyName, sw.Serial)

	// Only the -restore flag opens a writable session; every other mode asks
	// the driver for read access only.
	writable := *restore != "" || *upload != ""
	if writable {
		fmt.Printf("\n*** WRITABLE SESSION — will modify %s / %s ***\n", *title, *profile)
	}
	err = switchmtp.WithSession(sw.PnPID, writable, func(s *switchmtp.Session) error {
		stores, err := s.Children(s.RootID())
		if err != nil {
			return fmt.Errorf("list stores: %w", err)
		}
		fmt.Printf("\nstores (%d):\n", len(stores))
		for _, st := range stores {
			fmt.Printf("  %-22s dir=%-5v id=%s\n", st.Name, st.IsDir, st.ID)
		}

		saves, err := findStore(s, stores, "Saves")
		if err != nil {
			return err
		}
		groups, err := s.Children(saves.ID)
		if err != nil {
			return err
		}
		fmt.Printf("\n%s groups:\n", saves.Name)
		for _, g := range groups {
			kids, err := s.Children(g.ID)
			if err != nil {
				return err
			}
			fmt.Printf("  %-20s %d titles\n", g.Name, len(kids))
		}

		installed, err := childByName(s, saves.ID, "Installed games")
		if err != nil {
			return err
		}
		titles, err := s.Children(installed.ID)
		if err != nil {
			return err
		}

		// Profile tally — this is the check that the second tree level really
		// is user profiles and not save-data types.
		tally := map[string]int{}
		for _, t := range titles {
			kids, err := s.Children(t.ID)
			if err != nil {
				return err
			}
			for _, k := range kids {
				tally[k.Name]++
			}
		}
		fmt.Printf("\nsecond-level folder tally across %d titles:\n", len(titles))
		type kv struct {
			k string
			v int
		}
		var rows []kv
		for k, v := range tally {
			rows = append(rows, kv{k, v})
		}
		sort.Slice(rows, func(i, j int) bool { return rows[i].v > rows[j].v })
		for _, r := range rows {
			fmt.Printf("  %-18s %d\n", r.k, r.v)
		}

		if *dump != "" {
			for _, t := range titles {
				if !strings.Contains(strings.ToLower(t.Name), strings.ToLower(*dump)) {
					continue
				}
				fmt.Printf("\n=== %s ===\n", t.Name)
				profs, err := s.Children(t.ID)
				if err != nil {
					return err
				}
				for _, p := range profs {
					start := time.Now()
					files, err := s.Walk(p.ID)
					if err != nil {
						return err
					}
					var total int64
					for _, f := range files {
						total += f.Size
					}
					fmt.Printf("  [%s] %d files, %d bytes (%s)\n", p.Name, len(files), total, time.Since(start).Round(time.Millisecond))
					for i, f := range files {
						if i >= 12 {
							fmt.Printf("      … %d more\n", len(files)-i)
							break
						}
						fmt.Printf("      %-52s %8d\n", f.Rel, f.Size)
					}
				}
			}
		}

		if *scan {
			start := time.Now()
			lib, err := switchsaves.Scan(s, sw, true, nil)
			if err != nil {
				return err
			}
			var profiles, files int
			var bytes int64
			seen := map[string]bool{}
			for _, t := range lib.Titles {
				for _, e := range t.Entries {
					if e.Kind == switchsaves.EntryProfile {
						profiles++
						seen[e.Name] = true
					}
					files += e.FileCount
					bytes += e.SizeBytes
				}
			}
			var names []string
			for n := range seen {
				names = append(names, n)
			}
			sort.Strings(names)
			fmt.Printf("\nscan: %d titles, %d profile-entries, %d files, %d bytes in %s\n",
				len(lib.Titles), profiles, files, bytes, time.Since(start).Round(time.Millisecond))
			fmt.Printf("  distinct profiles: %s\n", strings.Join(names, ", "))
		}

		if *backup != "" {
			if *root == "" {
				return fmt.Errorf("-backup requires -root")
			}
			start := time.Now()
			r, err := switchsaves.BackupEntry(s, sw, *root, *backup, *profile, "manual", "harness")
			if err != nil {
				return err
			}
			fmt.Printf("\nbackup %s / %s\n  files=%d bytes=%d skipped=%v in %s\n  -> %s\n",
				r.Title, r.Profile, r.FileCount, r.TotalBytes, r.Skipped,
				time.Since(start).Round(time.Millisecond), r.ArchivePath)
		}

		if *plan != "" {
			if *title == "" {
				return fmt.Errorf("-plan requires -title <console title> to compare against")
			}
			p, err := switchsaves.PlanUpload(s, *plan, *title, *profile)
			if err != nil {
				return err
			}
			fmt.Printf("\nDRY RUN plan %s -> %s / %s\n", *plan, p.Title, p.Profile)
			fmt.Printf("  would replace %d, add %d, total %d bytes; %d device files untouched\n",
				p.Replaced, p.NewFiles, p.TotalBytes, len(p.Untouched))
			for i, w := range p.Writes {
				if i >= 10 {
					fmt.Printf("    … %d more\n", len(p.Writes)-i)
					break
				}
				verb := "add    "
				if w.Replaces {
					verb = "replace"
				}
				fmt.Printf("    %s %-44s %8d\n", verb, w.Rel, w.Size)
			}
			fmt.Println("  (nothing was written to the console)")
		}

		if *hash != "" {
			entry, err := switchsaves.FindEntry(s, *hash, *profile)
			if err != nil {
				return err
			}
			files, err := s.Walk(entry.ID)
			if err != nil {
				return err
			}
			sort.Slice(files, func(i, j int) bool { return files[i].Rel < files[j].Rel })
			fmt.Printf("\nstate of %s / %s (%d files):\n", *hash, *profile, len(files))
			for _, f := range files {
				rc, err := s.Open(f.ID)
				if err != nil {
					return err
				}
				h := sha256.New()
				n, err := io.Copy(h, rc)
				rc.Close()
				if err != nil {
					return err
				}
				fmt.Printf("  %-46s %8d  %s\n", f.Rel, n, hex.EncodeToString(h.Sum(nil)))
			}
		}

		if *restore != "" {
			if *title == "" || *root == "" {
				return fmt.Errorf("-restore requires -title and -root (root is where the safety backup goes)")
			}
			start := time.Now()
			plan, err := switchsaves.RestoreToConsole(s, sw, *root, *restore, *title, *profile, "harness", nil)
			if err != nil {
				return fmt.Errorf("restore failed: %w", err)
			}
			fmt.Printf("\nrestored onto %s / %s in %s\n  replaced=%d added=%d bytes=%d untouched=%d\n",
				*title, *profile, time.Since(start).Round(time.Millisecond),
				plan.Replaced, plan.NewFiles, plan.TotalBytes, len(plan.Untouched))
		}

		if *upload != "" {
			if *title == "" {
				return fmt.Errorf("-upload requires -title")
			}
			start := time.Now()
			plan, err := switchsaves.Upload(s, *upload, *title, *profile, nil)
			if err != nil {
				return fmt.Errorf("upload failed: %w", err)
			}
			fmt.Printf("\nuploaded %s -> %s / %s in %s\n  replaced=%d added=%d bytes=%d\n",
				*upload, *title, *profile, time.Since(start).Round(time.Millisecond),
				plan.Replaced, plan.NewFiles, plan.TotalBytes)
		}

		if *read != "" {
			parts := append([]string{saves.Name, "Installed games"}, strings.Split(*read, "/")...)
			obj, err := s.Resolve(parts...)
			if err != nil {
				return err
			}
			rc, err := s.Open(obj.ID)
			if err != nil {
				return err
			}
			defer rc.Close()
			h := sha256.New()
			start := time.Now()
			n, err := io.Copy(h, rc)
			if err != nil {
				return fmt.Errorf("read: %w", err)
			}
			fmt.Printf("\nread %s\n  %d bytes in %s\n  sha256 %s\n",
				*read, n, time.Since(start).Round(time.Millisecond), hex.EncodeToString(h.Sum(nil)))
		}
		return nil
	})
	if err != nil {
		fail("%v", err)
	}
	fmt.Println("\nOK")
}

// findStore matches the DBI store naming, which prefixes an index ("7: Saves").
func findStore(s *switchmtp.Session, stores []switchmtp.Object, want string) (switchmtp.Object, error) {
	for _, st := range stores {
		if strings.Contains(strings.ToLower(st.Name), strings.ToLower(want)) {
			return st, nil
		}
	}
	return switchmtp.Object{}, fmt.Errorf("store %q not found", want)
}

func childByName(s *switchmtp.Session, parentID, name string) (switchmtp.Object, error) {
	o, ok, err := s.Child(parentID, name)
	if err != nil {
		return switchmtp.Object{}, err
	}
	if !ok {
		return switchmtp.Object{}, fmt.Errorf("%q not found", name)
	}
	return o, nil
}

func fail(f string, a ...any) {
	fmt.Fprintf(os.Stderr, "gs-switch: "+f+"\n", a...)
	os.Exit(1)
}
