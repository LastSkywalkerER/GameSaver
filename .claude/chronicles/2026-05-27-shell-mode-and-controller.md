# 2026-05-27 — Tray, playtime, controller, shell mode, PS UI

The arc from "windowed utility" to "console-style launcher".

## What landed (v0.3.x → v0.6.x)
- **Tray + watcher + custom icon** (v0.3.x): close-to-tray, fsnotify auto-backup, multi-res ICO, playtime tracker (tasklist polling), sort options, per-game session history, tile date chips + Settings toggles, install-folder sizes.
- **Single-instance lock** (v0.3.7), **close-freeze fix** via OnBeforeClose (v0.3.8), **periodic + manual update banner** (v0.3.9).
- **Xbox controller** via XInput (v0.4.0).
- **Shell-replacement mode + watchdog** (v0.4.1): register as Windows shell, downloadable watchdog, escape hotkey.
- **Themed Modal** replacing window.confirm (v0.4.2); shell frameless-fullscreen + clean exit (v0.4.3).
- **UI cleanup** (v0.4.4): collapsible sidebar, dropped redundant Library page + filter select, system-locale default.
- **PlayStation-style shell UI** (v0.5.0): animated background, hero + carousel, corner icons, procedural sound; hero/carousel gap + keyboard/wheel nav (v0.5.1); controller chip in shell (v0.5.2).
- **Monitor picker** (v0.6.0) + **passwordless auto-logon** (v0.6.1); controller-mount fix, hot-plug, sound packs (v0.6.2); monitor-disable DEVMODE fix + picker UX + OS audio link (v0.6.3); multi-slot XInput + mmsys.cpl (v0.6.4); picker order + live playtime refresh (v0.6.5).

## Theme of the phase
Most effort went into making the shell experience robust against Windows realities: foreground locks,
display topology churn, XInput contention, missing Explorer. The recurring lesson — **our own actions
emit events that re-trigger our actions** — drove the structured-reasoning protocol.
