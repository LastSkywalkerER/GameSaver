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
- **Full replace, not merge (revised after v0.8.0).** GameSaver IS the menu, so the old app list has
  no value — and a name-merge created duplicates (Sunshine's "Alan Wake 2" vs our canonical "Alan
  Wake II"). Sync now writes `[Desktop, ...ourGames]`: it keeps only the special **Desktop** entry
  (so desktop streaming still works) and drops everything else. Clear resets to `[Desktop]`. The
  managed-names sidecar from v0.8.0 was removed — we own the whole file now.
- **image-path → PNG (revised after v0.8.0).** Moonlight reliably renders only PNG box art, but ~80%
  of our cached covers are JPG, so they didn't show. We transcode each cover to PNG into
  `cache\sunshine-art\` and point image-path at the absolute PNG (PNG covers are used as-is). Decode
  failure → no box art for that entry rather than a failed sync.
- **Elevated write.** On a default install `apps.json` is in `Program Files` (Users have RX, not W).
  We stage the merged file to `%LOCALAPPDATA%\GameSaver\` (no admin) then do a single UAC-elevated
  `cmd /c copy` (ShellExecuteEx `runas`), waiting on the process + checking its exit code so a
  cancelled UAC doesn't report success. Same elevation pattern as [0018](0018-autologin-via-netplwiz.md).

## Consequences

- One UAC prompt per Sync/Clear. The elevated step is a generated `.bat` that does copy → stop service
  → kill orphan `sunshine.exe` → start service → verify RUNNING, because **Sunshine caches its app
  list in memory and only re-reads apps.json on (re)start** (the v0.8.0 "buttons do nothing" report).
  The orphan-kill (v0.8.2) fixes Sunshine dying on restart: a lingering `sunshine.exe` held the port,
  so the freshly-started service couldn't bind and died.
- **Progress is streamed, not hidden.** The batch logs each step to a file; GameSaver tails it and
  emits `sunshine:progress` lines into a modal log (copy → stop → kill → start → verify), so the user
  sees what's happening instead of a frozen button. (v0.8.2, per user request.)
- The restart drops any active stream for ~2 s. Acceptable since Sync/Clear are explicit desktop
  actions, surfaced in the UI text.
- After sync the user still refreshes the app grid in Moonlight (client-side cache).
- JSON is written with `SetEscapeHTML(false)` so deep-link `&` stays literal, and 4-space indent to
  match Sunshine's own formatting.

## Alternatives rejected

- **Sunshine HTTPS API (47990):** no elevation, but needs the user's Sunshine credentials + self-signed
  cert handling. Heavier; revisit if the elevated-copy UX annoys.
- **Copy covers into `assets/`:** more files to manage + another elevated write; absolute image-path is
  simpler.
