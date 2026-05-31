# 0024 — Shell-mode background art (video → image) + ambient drone

**Status:** accepted · 2026-05-31 · ships in v0.9.2

## Context

The v0.5 shell background was a fixed deep-purple gradient with three slow-drifting blurred blobs.
Pretty, but generic — every game looked the same. Users with consoles in front of them are used to
PS5 / Apple TV / Steam Big Picture, where the front page screams "the game you're hovering over"
through full-bleed art (often a looped clip). The fixed gradient looked cheap by comparison.

Two parts to close the gap:

1. **Art that follows the active tile.** Whatever game is under the carousel cursor should fill the
   background. Light overlay so the art is actually visible.
2. **Animated when possible.** SteamGridDB exposes "animated heroes" (webm/mp4 looped clips of the
   game) for a chunk of the library — when present, use them instead of the still hero.

Separately, the menu was silent except for navigation tones — turning on the TV and hearing nothing
feels dead. A very quiet ambient pad gives the same "the system is alive" feeling as a console boot
chord, without needing audio assets (the rules forbid them — see `frontend-ui.md`).

## Decision

### Backend: per-game `VideoPath`
- New `games.video_path TEXT NOT NULL DEFAULT ''` column, added via the idempotent `extraColumns`
  retrofit (decisions/0002 / `storage-sqlite.md`). Existing DBs upgrade in place.
- `domain.Game.VideoPath` added; `UpsertGame` / `gameSelectCols` / `scanGame` /
  `UpdateGameCovers` carry it.
- `meta.fetch()` now returns `(cover, hero, icon, video)`. For Steam-ID-known games it queries
  SGDB's `heroes/steam/<appid>?types=animated`; for name-matched games, `heroes/game/<id>?types=animated`.
  The first asset (webm/mp4) is downloaded by the existing `tryDownload` which is content-keyed
  by URL hash so re-runs don't re-fetch.
- `EnrichAll`'s "already complete" skip-gate now requires `cover && hero && video` so existing rows
  get their video on next enrich pass. `tryDownload` is a no-op when the file is already on disk,
  so this is cheap.
- `guessExt` recognises `webm` / `mp4` content-types and extensions.

### Frontend: `ShellBackground(game)`
Three-tier render, by priority:
1. `<video src={videoUrl(game.videoPath)} autoplay muted loop playsInline>` — autoplay is allowed
   because it's muted. `key={game.id}` forces remount on game switch so the animation restarts and
   the crossfade triggers.
2. `<img src={heroUrl ?? coverUrl}>` — both shown until the video fires `playing`, so we never
   flash a black frame while the webm is parsing. `videoReady` state cross-fades them.
3. The old drifting blobs — kept as the **empty-library** fallback only.

Overlay tuned WAY lighter than v0.5: a vertical gradient from `black/15` at top to `black/55` at
bottom, plus a soft vignette (`transparent 55%` → `rgba(0,0,0,0.55)` at corners). The art now reads
through; the bottom edge stays dark enough for the carousel plate.

`HeroPanel` lost its own duplicated `<img>` — the background owns the art now. The title/buttons
keep a local left-side gradient (`black/80 → transparent` over the left two-thirds) so text stays
readable against busy heroes.

### Ambient drone (`sound.ts`)
Procedural Web Audio pad, no asset files. Three sine oscillators at A2/E3/G3 (an A-minor-7
fragment, picked so the highest voice sits below speaking-voice register and doesn't fight a
voice-call layered on top), a low-pass at 900 Hz for warmth, a 0.07 Hz LFO on master gain for
"breathing", master capped at `0.012` — quiet enough that you only notice when it stops.

Lifecycle in `ShellApp`:
- AudioContext autoplay is gated until a user gesture. We arm `pointerdown`/`keydown`/`wheel` once
  to kick start, then remove those listeners — `visibilitychange` runs the suspend/resume loop
  thereafter.
- Stops on `document.hidden` (window minimised — i.e. game launched), resumes on visible.
- `subscribeSoundPack` flips it off when the user picks "off" in Settings, on otherwise.

## Consequences

- The shell is now obviously alive — both visually (looped hero clips) and audibly (soft pad).
- Bandwidth: animated heroes can be 2–8 MB. Cached on first sight; re-runs use the file from disk.
  Worst case for a fresh library of 100 Steam games is roughly a few hundred MB — same order as the
  already-fetched hero stills.
- Performance: a single `<video>` decoding muted webm on the desktop is a few % of one CPU core
  and barely registers on a discrete GPU. Multiple tiles still use stills.
- Migration of `games.video_path` is idempotent — old DBs open fine, the column is added on the
  next launch with empty default, and the next `EnrichAll` populates it.

## Alternatives considered

- **Steam micro-trailers (`microtrailer.webm` on the Steam CDN)**: tempting because no API key
  needed, but the URL pattern requires the trailer's movie ID, which is not derivable from the
  appid — needs a store API call. SGDB animated heroes already use our existing key + fetch path,
  so they win on simplicity.
- **A static still pulsing zoom (Ken Burns) instead of a real video**: cheaper to ship, but the
  user explicitly asked for "видос" — a real motion clip. Ken Burns is a fallback we may add
  later for games SGDB doesn't have animated heroes for.
- **A real audio asset for the ambient pad**: rejected — the repo's sound rule (`frontend-ui.md`)
  is "no audio files". Procedural keeps the convention.
