# Rule — Save matching & scanning

The job: given a discovered game, find its real save folder(s) on disk, without false positives that
would back up the wrong data.

## Sources of truth

- **Ludusavi manifest** (`internal/match/data`, `go:embed`) — 53k+ games, each with save-path globs and hub placeholders (`<winAppData>` etc.). Embedded, **sanitized** of any hardcoded creds (see [`secrets.md`](secrets.md)).
- **Forward match** — game → manifest entry → resolve globs → candidate save dirs.
- **Reverse scan** — walk known save hubs (AppData, Documents\My Games, Saved Games, etc.) and match folders back to games; discovers saves for games the forward pass missed and can create new game/location rows.

## Matcher rules (hard-won; don't "simplify" the thresholds)

- **Path-scored matching** for ambiguous names: prefer manifest entries whose resolved path actually exists / overlaps the install. A bare name match with no `Files` is rejected (it created the Apex orphan card).
- **Aliases / canonical naming:** the `aliasOf` map tracks renames (e.g. *Alan Wake 2* → *Alan Wake II*). Reverse scan skips aliases; `matchGame` returns the canonical key. Prevents duplicate cards.
- **Tokenizer:** split CamelCase **before** lowercasing (the *DeathStranding* bug). Order matters.
- **Coalesce-to-root safety:** when merging matched subfolders up to a common parent, **block** the coalesce if non-matched content > 100 MB AND the parent is > 3× the matched bytes. Tuned so Apex (62 files in the save folder) coalesces but a giant unrelated parent doesn't get swallowed.
- **Hub matching is prefix-based** (`\AppData\Local`), not segment-based — generic folder names like `local`/`profile` are not standalone hubs.

## Pirate / repack scan (`internal/scan/pirate`)

- Extends coverage beyond Ludusavi for cracked installs. **Strict name matching** only — pirate folders are noisy, so the bar for a match is higher than for launcher installs.
- Vendor blacklist filters known non-game folders (had a duplicate-key bug once — keep the map keys unique).

## Save-size measurement

- 🔴 **Size a save location by walking its own tree, never its parent.** Incident: a single-file stat fell back to the parent dir and reported 60 GB "saves". `util.DirSizeAndCount` walks the exact path.

## Manual override

- The user can re-pick the manifest entry for a game (`RematchGame`) or add a save folder manually (deep scan / "Папка сейва"). Manual choices win over auto-matching.
