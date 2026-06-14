# 0029 — Mute all launcher audio while minimized behind a game

**Status:** accepted · 2026-06-13 · ships in v0.10.9

## Context

After launching a game from the shell, `doLaunch` calls `MinimizeSelf()` and the shell drops out of
the way — but the **ambient pad kept playing** over the running game (and any nav tone would too).

`ShellApp` already had a `visibilitychange` handler meant to stop the ambient when hidden
(`onVis: if (document.hidden) stopAmbient()`). It never fired: **WebView2 does not deliver
`visibilitychange` / set `document.hidden` on an OS-level window minimize.** So the only signal we
had for "we're hidden" was dead for the exact case that matters.

## Decision

Don't depend on `visibilitychange` for minimize. Mute **explicitly** from the minimize/restore
cycle we control, and mute *everything* at once via a master gain.

`sound.ts`:
- All output (nav tones + ambient) now routes through a single **master `GainNode`** instead of
  connecting straight to `ctx.destination`.
- New `setMuted(m)` ducks the master gain to 0 (m=true) or back to 1 (m=false) with short
  exponential ramps (0.2 s down / 0.35 s up — click-free), and gates new nav tones (`play()` early-
  returns while muted). The ambient **source keeps running** while muted, so it resumes seamlessly
  on unmute. Safe to call before any sound exists (the flag seeds the master gain on creation).

`ShellApp.tsx`:
- `doLaunch`: `setMuted(true)` right after `MinimizeSelf()`.
- `playtime:changed {endedAt}` handler: `setMuted(false)` + `startAmbient()` (the latter no-ops if
  the pad survived; rebuilds it if a stray real `visibilitychange` had torn it down).

The legacy `onVis` stop/start path is left in place as a harmless secondary net for genuine
visibility changes.

## Consequences

- Game launch now silences the whole launcher; game exit (≤6 s later, on the playtime poll) fades it
  back — mute lifecycle tracks the existing window minimize/restore lifecycle exactly.
- Master gain is a reusable choke point: any future sound routed through it is covered by mute for
  free.

## Notes / scope

- The **GameDrawer** "launch" button (shell `details` overlay) calls `LaunchGame` **without**
  `MinimizeSelf`, so it doesn't minimize and isn't muted — the launcher stays on screen, so ambient
  playing there is correct. (That path arguably should minimize too, but that's a separate behavior
  change, out of scope here.)
- 🔴 Don't "simplify" this back to `visibilitychange`-only — it's dead on WebView2 minimize. If a
  future minimize trigger is added that doesn't go through `doLaunch`, route its mute through
  `setMuted` as well (or have the backend emit a minimize/restore event both can subscribe to).
