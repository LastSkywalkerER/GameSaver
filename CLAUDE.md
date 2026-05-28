# CLAUDE.md — GameSaver

> **Repo-level file** for `GameSaver` — a native **Windows** desktop app (Wails v2 + Go + React/TS)
> that scans drives for installed games, matches & versions their save folders, backs them up as
> ZIPs, and can run as a PlayStation-style **shell replacement** for a console-like experience.
> Version: v1.0 · Last reviewed: 2026-05-28 · Current release: **v0.7.8**
>
> This file is **navigation and red lines**. Detailed rules — in `.claude/rules/`.
> Claude workflow — in `.claude/protocol/`. Decisions — in `.claude/decisions/`.
> Cross-session context — in `.claude/handoffs/`, milestones in `.claude/chronicles/`,
> dead ends in `.claude/attempts/`.

---

## 0. How this file works (read first)

This file is alive. It grows after incidents and decisions.

- **This file is the single entry point** for the rules. If a rule is not mentioned here and is not in `.claude/rules/` — it does not exist.
- **After every incident** (a bug shipped, Claude did the wrong thing, the user had to re-test something) — add a rule to §6 Red Lines or the relevant `.claude/rules/*.md`, with a one-liner about the incident.
- **After every non-trivial decision** — write `.claude/decisions/NNNN-slug.md`.
- **Every shipped change is a tag.** There is no "merge then release later" — a fix is not done until its `vX.Y.Z` tag built green on CI and published a release. See [`protocol/build-verify-ship.md`](.claude/protocol/build-verify-ship.md).
- **If a rule contradicts the code** — do not guess. Stop and ask: "rule §X says A, code does B — which is the truth?"

---

## 1. Repository map (`.claude/` index)

```
.claude/
├── README.md                          # how this folder is organized
│
├── rules/                             # CODE RULES (do / don't)
│   ├── build-release.md               # wails build, tag→CI→release, ldflags, watchdog asset
│   ├── windows-syscalls.md            # Win32 struct ABI, XInput, ChangeDisplaySettingsEx, negative ints
│   ├── shell-mode.md                  # GS_SHELL_MODE, watchdog, escape hatch, minimize/pause cycle
│   ├── launcher.md                    # exe-vs-deeplink priority, protocol registration check
│   ├── save-matching.md               # Ludusavi manifest, path-scored matcher, aliases, coalesce, pirate
│   ├── backup-restore.md              # versioned zips, sidecar manifest, reconcile, data safety
│   ├── storage-sqlite.md              # modernc driver, idempotent migrations
│   ├── frontend-ui.md                 # React/TS, i18n, controller hooks, sound packs, Modal, prefs
│   ├── secrets.md                     # SteamGridDB key — never in source/logs/files
│   └── code-style.md                  # Go + TS idioms, comments, the "why" not "what"
│
├── protocol/                          # CLAUDE WORKFLOW (how Claude operates)
│   ├── build-verify-ship.md           # the loop: wails build → commit → tag → watch CI → verify release
│   ├── proof-loop.md                  # Claude does not confirm its own work
│   ├── structured-reasoning.md        # format for debugging the recurring "topology/focus loop" bugs
│   └── self-maintenance.md            # when to write decision/chronicle/handoff/attempt
│
├── decisions/                         # ADR-lite: one decision = one file
│   ├── 0001-stack-wails-go-react.md
│   ├── 0002-sqlite-modernc-cgo-free.md
│   ├── 0003-ludusavi-manifest-embedded.md
│   ├── 0004-path-scored-matcher-and-aliases.md
│   ├── 0005-reverse-scan-and-pirate-scan.md
│   ├── 0006-backup-zip-sidecar-reconcile.md
│   ├── 0007-autoupdate-github-releases.md
│   ├── 0008-steamgriddb-key-ci-injection.md
│   ├── 0009-single-instance-lock.md
│   ├── 0010-close-to-tray-onbeforeclose.md
│   ├── 0011-shell-replacement-watchdog.md
│   ├── 0012-watchdog-downloaded-on-demand.md
│   ├── 0013-xinput-controller-no-cgo.md
│   ├── 0014-monitor-management-changedisplaysettings.md
│   ├── 0015-monitor-picker-spans-virtual-desktop.md
│   ├── 0016-ps-style-shell-ui.md
│   ├── 0017-procedural-sound-web-audio.md
│   ├── 0018-autologin-via-netplwiz.md
│   ├── 0019-sleep-setsuspendstate-direct.md
│   ├── 0020-launch-exe-over-deeplink.md
│   ├── 0021-sunshine-apps-json-sync.md
│   └── 0022-multi-location-discovery-and-migration.md
│
├── chronicles/                        # PROJECT MILESTONES (how we got here)
│   ├── 2026-05-26-mvp-m0-m11.md
│   ├── 2026-05-27-shell-mode-and-controller.md
│   └── 2026-05-28-shell-hardening.md
│
├── handoffs/                          # SESSION SUMMARIES (done, didn't work, next)
│   ├── README.md
│   └── 2026-05-28-shell-launch-cycle.md
│
├── attempts/                          # FAILED APPROACHES (don't re-try dead ends)
│   ├── README.md
│   ├── hidewindowonclose-freeze.md
│   ├── devmode-struct-padding.md
│   └── pause-emit-but-keep-polling.md
│
└── audit/                             # doc-drift / consistency notes
    └── README.md
```

---

## 2. Quick orientation

- **What it is:** GameSaver — a Windows game-save manager + console-style game launcher.
- **Repo:** `git@github.com:LastSkywalkerER/GameSaver.git`, branch `main`. Owner: LastSkywalkerER.
- **Distribution:** GitHub Releases, portable `.zip`, in-app self-updater. **No installer, no code-signing** (see [decision 0007](.claude/decisions/0007-autoupdate-github-releases.md)).
- **Platform:** Windows only. Most of `internal/` is `*_windows.go` and uses Win32 syscalls.
- **Language:** Go 1.25 (backend) + TypeScript/React 18 + Tailwind (frontend), bridged by Wails v2.12.0.
- **CI:** GitHub Actions on tag push (`v*`) → builds, packages, publishes release. See [`build-release.md`](.claude/rules/build-release.md).

### 2.1 What it does

1. **Scan** all drives for installed games across launchers (Steam/GOG/Epic/EA/Ubisoft/Battle.net/Xbox/Riot/Minecraft) + heuristic disk scan + pirate/repack detection.
2. **Match** each game to its save folders via the embedded **Ludusavi manifest** (53k+ games), plus a **reverse scan** that walks known save hubs to discover saves for games not yet matched.
3. **Back up / restore** saves as versioned ZIP snapshots with sidecar manifests; retention by keep-N.
4. **Metadata**: covers/hero art from SteamGridDB + Steam CDN + extracted exe icons.
5. **Playtime tracking** via `tasklist` polling (last-played, total seconds, per-game sessions).
6. **Shell mode**: register as the Windows shell for a fullscreen, controller-driven, PS-like launcher.

---

## 3. Module map (where things live)

### Backend — `internal/`

| Package | Owns |
|---|---|
| `domain` | Core types (`Game`, `Installation`, `SaveLocation`, `Snapshot`, `GameView`, `SourceKind`). |
| `config` | `AppConfig` load/save (`settings.json` in user data dir). Empty `Language` = use system locale. |
| `storage/sqlite` | modernc.org/sqlite store + **idempotent** migrations (`addColumnIfMissing`). |
| `scan/launchers` | Per-launcher install discovery (Steam/GOG/Epic/EA/Ubisoft/Xbox/Riot/...). |
| `scan/heuristic` | Generic disk walk for installs not registered with any launcher. |
| `scan/pirate` | Pirate/repack detection + strict-name save scan. |
| `scan/pipeline` | Orchestrates launchers + heuristic into one scan run, emits progress events. |
| `scan/dirsize` | Lazy background install-dir size walker (emits `inst:size`). |
| `match` | Ludusavi manifest matcher (path-scored), reverse scan, canonical naming + aliases, manual override. |
| `match/data` | Embedded Ludusavi manifest (sanitized — see [secrets.md](.claude/rules/secrets.md)). |
| `meta` | Cover/hero/icon enrichment (SteamGridDB, Steam CDN, exe icon extraction). |
| `backup` | ZIP snapshot engine, restore (pre-restore auto-backup), retention, reconcile. |
| `launcher` | Game launch: exe-first for non-Steam, deep-link only if protocol registered. |
| `playtime` | `tasklist`-poll session tracker (6 s interval); emits `playtime:changed`. |
| `watcher` | fsnotify auto-backup with debounce (default off). |
| `controller` | XInput poller (all 4 slots), connect/button/nav events, pause while gaming. |
| `display` | Monitor enumerate/disable-others/restore via `ChangeDisplaySettingsExW`; hot-plug watch. |
| `winutil` | Span our window across the virtual desktop / snap to a monitor (`SetWindowPos`). |
| `power` | `LockWorkStation` + `SetSuspendState` (sleep with wake events disabled). |
| `autologin` | Passwordless Windows logon helper — unhide netplwiz checkbox + launch it (never touches password). |
| `shellmode` | Registry shell registration + on-demand watchdog download (SHA-256 verified). |
| `sunshine` | Sync the library into Sunshine's `apps.json` (cmd/cover) + clear; elevated write, preserves user entries. See [decision 0021](.claude/decisions/0021-sunshine-apps-json-sync.md). |
| `tray` | systray (skipped in shell mode). |
| `updater` | GitHub-release self-update (minio/selfupdate), `.old` cleanup. |
| `util` | `DirSizeAndCount`, hashing, etc. |

### Entry points — `cmd/` + root

| Path | Role |
|---|---|
| `main.go` | Wails bootstrap, window options, single-instance lock, shell-mode branch. |
| `app.go` | The Wails-bound `App` — every exported method is callable from the frontend. |
| `helpers_windows.go` | `openInExplorer`, etc. |
| `cmd/gamesaver-watchdog` | Tiny supervisor exe registered as the shell; restarts GameSaver, escape hotkey. |
| `cmd/gen-icon` | Generates multi-res `build/appicon.png` + `internal/tray/icon.ico`. |
| `cmd/gs-reconcile` | CLI backup reconcile. |
| `cmd/gs-smoke` | Smoke-test harness. |

### Frontend — `frontend/src/`

| Path | Role |
|---|---|
| `App.tsx` | Root. Branches: normal desktop UI vs `<ShellApp>` (when `runningAsShell`). Wires all Wails events. |
| `api.ts` | Re-exports generated Wails bindings + typed shapes + format helpers. |
| `i18n.ts` | ru/en dicts; default = system locale until user picks one. |
| `controller.ts` | `useControllerConnected/Nav/Button` hooks over `controller:*` events. |
| `sound.ts` | Procedural Web Audio sound packs (psstyle/subtle/retro/off). |
| `tilePrefs.ts` | Per-tile chip visibility prefs (localStorage). |
| `components/` | `Sidebar`, `TopBar`, `GameTile`, `GameDrawer`, `Modal` (+ `confirmModal`), `Toaster`, `UpdateBanner`, ... |
| `components/shell/` | `ShellApp`, `ShellBackground`, `HeroPanel`, `GameCarousel`, `CornerIcons`, `PowerMenu`, `MonitorPicker`. |
| `pages/` | `DashboardPage`, `BackupsPage`, `SettingsPage`. |

---

## 4. Architecture invariants (must-know on any task)

- **Windows-only.** Win32-touching files are `*_windows.go`. Struct layouts must match the Win32 ABI **byte for byte** (see [windows-syscalls.md](.claude/rules/windows-syscalls.md) — the `devModeW` 224-vs-220 incident).
- **Backend owns the data, frontend is a view.** All mutation goes through `app.go` Wails methods. The frontend never writes SQLite or the filesystem directly.
- **Events are one-way backend→frontend.** Backend emits via `wailsruntime.EventsEmit`; the frontend subscribes via `EventsOn`. Wails does NOT replay missed events — for state a late subscriber needs, expose a getter method (e.g. `IsControllerConnected`, `GetShellModeStatus`) and pull it on mount. (See [decision 0013](.claude/decisions/0013-xinput-controller-no-cgo.md).)
- **Two UIs, one app.** `GS_SHELL_MODE=1` (set by the watchdog) switches the whole React tree to `ShellApp` and changes window/tray behavior in `main.go`. See [shell-mode.md](.claude/rules/shell-mode.md).
- **The watchdog owns the shell slot, not GameSaver.exe.** GameSaver is downloaded-once-then-stable; the watchdog (separate, small, downloadable) is what the registry points at. See [decisions 0011](.claude/decisions/0011-shell-replacement-watchdog.md)/[0012](.claude/decisions/0012-watchdog-downloaded-on-demand.md).
- **The controller is shared, but contended.** Only one component-tree reacts to it at a time; while a game runs we **stop polling XInput entirely** so the game has the pad. See [decision 0013](.claude/decisions/0013-xinput-controller-no-cgo.md) and [attempts/pause-emit-but-keep-polling.md](.claude/attempts/pause-emit-but-keep-polling.md).
- **Display changes are self-inflicted.** Disabling monitors triggers `display:changed`, which must not re-trigger the picker (settling window) or abort a sleep (suppression window). See [decision 0015](.claude/decisions/0015-monitor-picker-spans-virtual-desktop.md).
- **Save data is sacred.** Restore auto-backs-up first; the DB can always be rebuilt from disk via reconcile (sidecar manifests). See [backup-restore.md](.claude/rules/backup-restore.md).

---

## 5. Tech stack & versions

| Concern | Tool | Notes |
|---|---|---|
| Desktop framework | **Wails v2.12.0** | WebView2 on Windows; bindings auto-generated into `frontend/wailsjs/`. |
| Backend language | **Go 1.25** (CI builds with 1.23) | |
| Frontend | **React 18 + TypeScript + Vite + Tailwind** | |
| DB | **modernc.org/sqlite v1.34.4** (pure-Go, no cgo for the driver) | see [decision 0002](.claude/decisions/0002-sqlite-modernc-cgo-free.md) |
| Tray | **getlantern/systray** | needs multi-res **ICO**, skipped in shell mode |
| Self-update | **minio/selfupdate** | atomic exe swap, `.old` cleanup |
| FS watch | **fsnotify** | auto-backup debounce |
| Save manifest | **Ludusavi** YAML, `go:embed` | sanitized of hardcoded creds |
| Controller | **XInput** (`xinput1_4.dll`) via syscall | no external dep |
| Display / window / power | **user32 / powrprof** via syscall | + `golang.org/x/sys/windows/registry` |
| Build | **`wails build`** (Wails CLI v2.12.0) | needs Go on PATH; ldflags inject version + key |
| CI/CD | **GitHub Actions** on tag push | `.github/workflows/release.yml` |
| Watchdog build | **pure Go**, `CGO_ENABLED=0`, `-H windowsgui` | shipped as a separate release asset |

> **Local build:** Go is at `C:\Program Files\Go\bin`, the Wails CLI at `C:\Users\Администратор\go\bin\wails.exe`. They are **not** on the default PATH of the bash tool — prepend `export PATH="/c/Program Files/Go/bin:$PATH"` and call wails by full path. See [build-release.md](.claude/rules/build-release.md).

---

## 6. 🔴 RED LINES (absolute prohibitions, no exceptions)

> Every rule here is blood on the wall, with the incident that caused it.

- 🔴 **Never put the SteamGridDB API key (or any secret) in source, logs, comments, commit messages, or any file under `E:\claude\` — including gitignored settings.** The user handed the key in chat once; it must only ever reach the final `settings.json` generated on the user's own machine, or the CI secret. Incident: key was nearly committed; manifest had hardcoded AWS creds → GitHub push protection block. See [secrets.md](.claude/rules/secrets.md).

- 🔴 **Win32 structs must match the C ABI exactly.** No "safety" padding fields. A wrong `sizeof` silently shifts every field after the mistake. Incident: `devModeW` was 224 B instead of 220 B → monitor resolutions read as garbage + `ChangeDisplaySettingsEx` returned `BADMODE (-2)`. See [windows-syscalls.md](.claude/rules/windows-syscalls.md) and [attempts/devmode-struct-padding.md](.claude/attempts/devmode-struct-padding.md).

- 🔴 **Never break the shell-mode escape hatches.** `Ctrl+Alt+Shift+F12` (watchdog global hotkey) and `gamesaver-watchdog.exe --disable-shell` must always work. A user with no taskbar and a crashed app has nothing else. See [shell-mode.md](.claude/rules/shell-mode.md).

- 🔴 **While a game is running, do NOT poll XInput.** Two processes hammering XInput on a wireless pad contend at the driver level and the game drops inputs. Pause = stop calling `XInputGetState`, not just stop emitting. Incident: v0.7.5 paused emit but kept polling → game saw no controller. See [decision 0013](.claude/decisions/0013-xinput-controller-no-cgo.md).

- 🔴 **`SetSuspendState` only via direct syscall, never `rundll32 ...,SetSuspendState`.** rundll32's entry **ignores its args** and sleeps with wake events enabled → PC wakes 2 s later. Incident: v0.7.0–0.7.6. See [decision 0019](.claude/decisions/0019-sleep-setsuspendstate-direct.md).

- 🔴 **"clean up / remove / filter" is never permission to delete user data.** Saves, snapshots, and the DB are the whole point of the app. Default = move/mark. Delete only on explicit "delete" wording.

- 🔴 **Restore must auto-backup the current save first.** Overwriting a live save without a pre-restore snapshot = data loss. See [backup-restore.md](.claude/rules/backup-restore.md).

- 🔴 **Never modify a shipped migration.** Add a new idempotent `addColumnIfMissing` / ALTER. Old DBs in the wild must still open. See [storage-sqlite.md](.claude/rules/storage-sqlite.md).

- 🔴 **Every fix ships as a new tag.** Don't tell the user "fixed" until `wails build` is green AND the `vX.Y.Z` CI run succeeded AND the release published. No `--amend` of a pushed commit; new commit, new tag. See [build-verify-ship.md](.claude/protocol/build-verify-ship.md).

- 🔴 **Claude does not confirm its own work.** "Builds fine, should work" without a green `wails build` (exit 0) and a confirmed CI conclusion is invalid. See [proof-loop.md](.claude/protocol/proof-loop.md).

- 🔴 **Deep-links only if the protocol is registered.** `cmd /c start <uri>` reports success even for an unregistered scheme (the shell then pops its own error). Check `HKCR\<scheme>` first. Incident: GOG games failed with `goggalaxy://`. See [launcher.md](.claude/rules/launcher.md).

- 🔴 **Do not change a "weird" value without understanding it.** e.g. the matcher's coalesce thresholds, the 30 s settling window, the 20 s sleep-suppress window, the playtime poll interval — each encodes a specific incident. Read the comment before "simplifying".

---

## 7. When in doubt

- A rule conflicts with another rule or with the code — **ask**, don't guess.
- The task touches Win32 syscalls, the watchdog, the matcher's safety thresholds, or backup/restore — re-read the relevant rule file first; these are the areas where mistakes are expensive (data loss / black-screen-no-shell).
- A bug looks like "the picker re-opens itself" / "sleep wakes instantly" / "controller acts in background" — it's almost always a **self-inflicted event loop** (our own action triggers `display:changed`/`controller:*` which re-triggers the action). Switch to [structured-reasoning.md](.claude/protocol/structured-reasoning.md) and look for the feedback loop, not the symptom.
- **A short answer is safer than a long one. If unsure — ask.**

---

## 8. References

- **GitHub:** https://github.com/LastSkywalkerER/GameSaver — releases, CI runs, tags.
- **Ludusavi manifest:** https://github.com/mtkennerly/ludusavi-manifest (source of the embedded save-path DB).
- **SteamGridDB:** https://www.steamgriddb.com/profile/preferences/api (cover art; key is CI-injected).
- **Wails v2 docs:** https://wails.io/docs/reference/runtime/intro
- **XInput:** https://learn.microsoft.com/windows/win32/api/xinput/
- **DEVMODEW / ChangeDisplaySettingsEx:** https://learn.microsoft.com/windows/win32/api/wingdi/ns-wingdi-devmodew
- **SetSuspendState:** https://learn.microsoft.com/windows/win32/api/powrprof/nf-powrprof-setsuspendstate

---

*File version: v1.0 · Update via commit `docs(claude): <reason>` with a one-liner trigger (incident/decision).*
