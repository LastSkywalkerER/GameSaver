# Rule — Shell-replacement mode

GameSaver can register as the **Windows user shell** (`HKCU\...\Winlogon\Shell`) so logon launches a
fullscreen, controller-driven, PS-style launcher instead of Explorer. This is powerful and dangerous.

## The pieces

- **`gamesaver-watchdog.exe`** (`cmd/gamesaver-watchdog`) — the binary the registry actually points at. Tiny pure-Go supervisor: spawns GameSaver, restarts it on crash (rate-limited 5/30 s, then MessageBox + give up), and listens for the global escape hotkey. Sets `GS_SHELL_MODE=1` in GameSaver's env. Downloaded on demand (see `decisions/0012`).
- **`internal/shellmode`** — registry read/write + on-demand watchdog download into `%LOCALAPPDATA%\GameSaver\bin\` (SHA-256 verified against the release `checksums.txt`). Writes `target.txt` (path to GameSaver.exe) for the watchdog.
- **`ShellApp`** (frontend) — the entire alternate React tree, shown when `runningAsShell`.

## 🔴 Escape hatches must never break

A user in shell mode has no taskbar, no Start menu, no tray. If the app wedges, these are the ONLY ways out:

1. **`Ctrl+Alt+Shift+F12`** — watchdog global hotkey: removes the registry value, launches `explorer.exe`, exits.
2. **`gamesaver-watchdog.exe --disable-shell`** — CLI, runnable from recovery / another machine session.
3. The in-app **🛑 Exit / power-menu Exit** — `RestoreMonitorConfig` → `DisableShellMode` → `QuitApp`; the watchdog sees a clean exit (code 0), declines to restart, launches Explorer.

Any change near shell mode must preserve all three. Test #1 every time.

## `GS_SHELL_MODE=1` behavior (in `main.go`)

- **Skip tray init** (no Explorer = no tray host; systray would hang).
- **Frameless + Fullscreen** window from start.
- **X button is a real exit** (`OnBeforeClose` returns false), not hide-to-tray.

## The game-launch cycle (the hard-won part)

In shell mode GameSaver is the fullscreen foreground window. Games launched via Steam/Epic/GOG are
started by their launcher (not the foreground process), so Windows' foreground lock keeps them BEHIND
us. The cycle:

1. **Launch** → `MinimizeSelf()`: minimize GameSaver AND `controller.SetPaused(true)`.
2. **Playing** → GameSaver is minimized and **not polling XInput at all**, so the game has the pad cleanly.
3. **Exit** → playtime tracker emits `playtime:changed {endedAt}` (6 s poll) → `RestoreSelf()`: unminimise + raise (always-on-top toggle) + `controller.SetPaused(false)`.

🔴 Pausing must **stop the XInput poll**, not just stop emitting events. See `decisions/0013`, `attempts/pause-emit-but-keep-polling.md`.

## Self-inflicted event loops (read before touching the picker or sleep)

Disabling monitors / sleeping changes display topology, which our `display.Watch` reports as
`display:changed`. Naively re-opening the picker on that event creates loops:

- **After a monitor pick:** 30 s **settling window** — `display:changed` during it silently re-asserts the chosen monitor instead of reopening the picker.
- **Before Lock/Sleep:** 20 s **suppression window** (wall-clock) — `display:changed` is ignored so the power-down churn doesn't reopen the picker or abort the sleep. Wake hours later is past the window → picker shows normally.

These windows are load-bearing. Don't shorten them without re-reading `decisions/0015`/`0019`.

## Monitor picker

- Shows on **every** shell launch + on `display:changed`, never silent-applies a remembered choice (a slept monitor can be dark-but-attached). Pre-selects the remembered monitor so it's one A-press.
- `PrepareMonitorPick` re-enables all monitors and spans our window across the virtual desktop; the picker renders one self-identifying copy per physical monitor ("📍 этот экран"). See `decisions/0015`.
- Re-triggerable any time via the 🖥 corner icon and the power-menu "Сменить монитор".
