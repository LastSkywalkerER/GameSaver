# 0014 — Monitor management via ChangeDisplaySettingsExW

Status: accepted
Date: 2026-05-27 (v0.6.0; fixed v0.6.3)

## Context
Shell mode should run on one "console" screen and turn the others off for the session, then restore them.

## Decision
Use the classic `EnumDisplayDevicesW` / `EnumDisplaySettingsExW` / `ChangeDisplaySettingsExW` path (not
the newer SetDisplayConfig): enumerate, snapshot the layout to `display-backup.json`, promote the chosen
monitor to primary (`CDS_SET_PRIMARY`), disable the rest (zero DEVMODE), commit, and restore on exit.

## Consequences
- Stable since XP; enough for "make exactly one active".
- Requires byte-exact `DEVMODEW` (220 B) — the 224-B padding bug returned BADMODE (attempts/devmode-struct-padding.md, 🔴 windows-syscalls.md).
- Can't disable the current primary without promoting the target first.
