# 0021 — Sync the library into Sunshine's apps.json

**Status:** accepted · 2026-05-28 · ships in v0.8.0

## Context

The user runs **Sunshine** (game-streaming host; Moonlight is the client). Its app registry is
`apps.json`: each entry has `name`, `cmd` (exe path or `steam://`/`com.epicgames.launcher://`
deep-link), `working-dir`, `image-path` (box art), and flags (`auto-detach`, `elevated`, …). GameSaver
already knows every game's name, exe, working dir, launch URI, source, and cover — the exact inputs
Sunshine wants. So GameSaver can populate Sunshine in one click.

## Decision

`internal/sunshine` (Windows-only). Settings shows a Sunshine block with **Sync games** / **Clear
games**; when Sunshine isn't found the block is rendered dimmed/disabled ("не задетекчен").

- **Detection:** find `sunshine.exe` under `%ProgramFiles%[(x86)]\Sunshine`; resolve `apps.json` via
  `file_apps` in `sunshine.conf` → `<install>\config\apps.json` → `%PROGRAMDATA%\Sunshine\config\apps.json`.
- **cmd priority mirrors `internal/launcher`:** Steam → `steam://` deep-link; everyone else → bare exe
  + `working-dir`; deep-link only as fallback. (Same reasoning as [0020](0020-launch-exe-over-deeplink.md).)
- **image-path:** absolute path to GameSaver's cached cover on disk — avoids copying into Sunshine's
  `assets/` and the bare-name-resolution question. (Sunshine accepts absolute paths; the user's own
  Cyberpunk entry already used one.)
- **Never clobber user entries.** We track the names WE added in a sidecar
  (`%LOCALAPPDATA%\GameSaver\sunshine-managed.json`). Sync upserts our names (and refreshes prior
  ones); Clear removes only sidecar names. Desktop / Steam Big Picture / hand-added apps are kept.
  We also stamp a best-effort `"gamesaver-managed":"true"` marker, but the sidecar is the source of
  truth because Sunshine's Web UI may drop unknown fields when it re-saves.
- **Elevated write.** On a default install `apps.json` is in `Program Files` (Users have RX, not W).
  We stage the merged file to `%LOCALAPPDATA%\GameSaver\` (no admin) then do a single UAC-elevated
  `cmd /c copy` (ShellExecuteEx `runas`), waiting on the process + checking its exit code so a
  cancelled UAC doesn't report success. Same elevation pattern as [0018](0018-autologin-via-netplwiz.md).

## Consequences

- One UAC prompt per Sync/Clear when `apps.json` is in Program Files. Acceptable; surfaced in the UI.
- Sunshine may need an app-list refresh in Moonlight (or a Sunshine restart on older builds) to show
  changes; we toast a hint rather than restarting the service (admin + interrupts active streams).
- JSON is written with `SetEscapeHTML(false)` so deep-link `&` stays literal, and 4-space indent to
  match Sunshine's own formatting.

## Alternatives rejected

- **Sunshine HTTPS API (47990):** no elevation, but needs the user's Sunshine credentials + self-signed
  cert handling. Heavier; revisit if the elevated-copy UX annoys.
- **Copy covers into `assets/`:** more files to manage + another elevated write; absolute image-path is
  simpler.
