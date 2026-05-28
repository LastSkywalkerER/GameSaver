# 2026-05-28 — Shell launch cycle + power + launcher fixes

## Shipped
- v0.7.0 power menu (Lock/Sleep/Exit)
- v0.7.1 picker on all monitors + every login + re-trigger
- v0.7.2 settling window (pick sticks)
- v0.7.3 suppress picker during Lock/Sleep
- v0.7.4 minimize on launch / restore on exit
- v0.7.5 pause controller while game runs
- v0.7.6 stop polling XInput while paused (game finally sees the pad)
- v0.7.7 PowerMenu click-under-cursor + sleep stays asleep (direct SetSuspendState)
- v0.7.8 launch via exe for non-Steam + protocol-registration check (GOG fix)

## Confirmed working by user
Monitor pick, sleep, game-launch-in-front, in-game controller — full cycle.

## Open / pending user test
- v0.7.8 GOG launch fix shipped; user to confirm Cyberpunk launches.

## Research-only (not a bug to fix)
- One game lost controller input even with GameSaver closed. Likely Steam Input / HidHide stuck state, or a per-game input config corrupted by an earlier force-kill from the focus-steal chaos in pre-0.7.5 builds. Suggested: fully restart Steam, replug pad, verify game files, reset that game's controller config. See chronicles/2026-05-28.

## Next ideas (deferred, with user)
- In-game overlay / gamepad-combo to minimize a running game (Steam-Overlay-class; researched, big lift, anti-cheat caveats). Not started.

## Watch out for
- Any change near the picker/sleep/controller is loop-prone — run protocol/structured-reasoning.md first.
- Don't shorten the 30 s settling / 20 s suppress / 6 s playtime / coalesce thresholds without reading why.
- This was the session that authored the whole `.claude/` tree.
