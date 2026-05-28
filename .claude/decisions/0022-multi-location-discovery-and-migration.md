# 0022 — Multi-location save discovery + migration between installations

**Status:** accepted · 2026-05-28 · ships in v0.9.0

## Context

Two gaps surfaced (LEGO Star Wars: The Skywalker Saga, Steam):

1. **A game can keep saves in more than one place** and we only registered the manifest's. LEGO's
   Ludusavi entry lists a Roaming `SAVEDGAMES` path (found) plus a `<winLocalAppData>/Packages/…/wgs`
   path tagged `store: microsoft` (correctly skipped for a Steam user). But the user ALSO had a save
   under `AppData\Local` that the manifest doesn't list for Steam — so it was never picked up, and
   even the per-game deep scan missed it.

2. **No way to move saves between two installations of the same game.** The user had pirate saves,
   then installed the Steam version (different save location), and wanted to pull the pirate saves in.

## Decision

### Multi-location: AppData-root mirroring (`match.mirrorAppDataRoots`)
After `applyManifestEntry` resolves the on-disk save paths, for each path under an AppData root
(Roaming / Local / LocalLow) we probe the **same relative `Publisher\Game\…` subpath — and its parent
game folder — under the OTHER two roots**, and register any that exist with files (SourceHint
`mirror`). Rationale: games routinely split saves/config across Roaming↔Local↔LocalLow under the
identical Publisher\Game folder; mirroring an exact relative path makes a false positive essentially
impossible (an unrelated game would have to reuse the same folder name in a different root).

### Migration: `backup.Engine.MigrateSave(gameID, fromLocID, toLocID)`
Copies one save location's tree over another's (merge; same-named files overwritten, dest-only files
kept). **Always takes a `preMigrate` backup of the whole game first** (data-safety red line) so the
destination is recoverable. Refreshes the destination's size/count/mtime afterward. Exposed as the
`MigrateSave` Wails method; GameDrawer shows a source→dest picker + "Перенести" when a game has ≥2
save locations. Confirmation modal spells out the overwrite + pre-backup.

## Consequences

- Mirroring runs inside the normal match pass — no extra user action; the Local twin shows up after a
  scan. SourceHint `mirror` distinguishes it from `ludusavi` matches in the UI.
- Migration reuses the snapshot engine for its safety net, so a bad migrate is one Restore away.
- Migration is copy-based (not a junction). The earlier `is_junction`/`junction_target` columns remain
  unused for now; a junction-based "single source of truth" mode could supersede this later.

## Alternatives considered

- **Auto-accept deep-scan hits during scan** to find extra locations: higher false-positive risk than
  exact-relative-path mirroring; rejected.
- **Junction the pirate save dir to the Steam one**: clever but fragile (NTFS-only, breaks if either
  side is recreated by the launcher); a plain copy with a pre-backup is more predictable.
