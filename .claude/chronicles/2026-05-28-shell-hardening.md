# 2026-05-28 — Shell hardening (the game-session cycle)

Polishing shell mode into something actually usable day-to-day.

## What landed (v0.7.x)
- **Power menu** — Lock / Sleep / Exit, controller (X) + corner icon (v0.7.0).
- **Picker on every screen + every login + re-trigger** (v0.7.1); **settling window** so a pick sticks (v0.7.2); **suppress picker during Lock/Sleep** (v0.7.3).
- **Game-session cycle:** minimize on launch + restore on exit (v0.7.4); pause controller while a game runs (v0.7.5); **stop polling XInput entirely** while paused so the game gets the pad (v0.7.6).
- **PowerMenu click target** (stale-closure fix) + **sleep immediate-wake fix** (direct SetSuspendState) (v0.7.7).
- **Launch fix:** prefer exe over deep-link for non-Steam; verify protocol registration (v0.7.8).

## Confirmed working by the user
Monitor picking + sleep + game launch (front) + in-game controller, end to end.

## Known open / research-only
- One specific game stopped seeing the controller even with GameSaver closed — likely Steam Input / HidHide stuck state, or a corrupted per-game input config from an earlier force-kill. Research notes given; not a GameSaver bug. (handoffs/2026-05-28-shell-launch-cycle.md)
- The "minimize a running game with a gamepad combo / in-game overlay" idea was researched and deferred (Steam-Overlay-class effort; foreground-lock + anti-cheat caveats).

## Doc state
This `.claude/` tree was authored at the end of this phase from the full session history.
