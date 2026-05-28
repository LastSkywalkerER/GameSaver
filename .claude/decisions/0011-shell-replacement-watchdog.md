# 0011 — Shell-replacement mode supervised by a watchdog

Status: accepted
Date: 2026-05-27 (v0.4.1)

## Context
Goal: a console-like experience where the PC boots straight into GameSaver instead of the desktop, via
`HKCU\...\Winlogon\Shell`. But if the registered shell crashes, the user has a black screen and no way out.

## Decision
Register a tiny **watchdog** (`gamesaver-watchdog.exe`) as the shell, not GameSaver itself. The watchdog:
- spawns GameSaver (`GS_SHELL_MODE=1`), restarts it on crash (rate-limited 5/30 s, then MessageBox + give up),
- listens for the global escape hotkey **Ctrl+Alt+Shift+F12** → unregister + launch Explorer + exit,
- supports `--disable-shell` from a recovery shell.

## Consequences
- Robust against GameSaver crashing (watchdog recovers or bails safely).
- Three escape hatches — all 🔴 must-never-break (rules/shell-mode.md).
- GameSaver in shell mode skips tray and goes frameless+fullscreen.
