# 0012 — Watchdog downloaded on demand, not bundled

Status: accepted
Date: 2026-05-27 (v0.4.1)

## Context
The watchdog is only needed by users who enable shell mode (a minority). Bundling it next to the main
exe bloats every download and complicates the portable zip.

## Decision
Ship the watchdog as a **separate GitHub release asset** (`gamesaver-watchdog-windows-amd64.exe`). When
the user enables shell mode, download it on demand into `%LOCALAPPDATA%\GameSaver\bin\`, verifying its
**SHA-256 against the release `checksums.txt`**. CI builds it pure-Go (`CGO_ENABLED=0`, `-H windowsgui`, ~2 MB).

## Consequences
- Main download stays small; shell mode is opt-in.
- The download path must verify the checksum before trusting the binary (it becomes the user's shell).
- `internal/shellmode` owns download + registry + `target.txt` (path to GameSaver.exe for the watchdog).
