# 0005 — Reverse scan + pirate/repack scan

Status: accepted
Date: 2026-05-26

## Context
Forward matching (game → manifest → path) misses: games not in the manifest, and cracked/repack
installs that no launcher knows about.

## Decision
- **Reverse scan:** walk known save hubs (AppData, Documents\My Games, Saved Games, ...) and match folders back to games; can create new game/location rows.
- **Pirate scan** (`internal/scan/pirate`): detect repack/cracked installs and match their saves with **strict** name matching (higher bar, because pirate folders are noisy), plus a vendor blacklist.

## Consequences
- Coverage well beyond the manifest.
- Reverse/pirate paths are the riskiest for false positives → strict matching + the same coalesce safety as forward matching.
- Save-size must be measured on the exact folder, never the parent (the 60 GB incident).
