# 0006 — Backups: versioned ZIP + sidecar manifest + reconcile

Status: accepted
Date: 2026-05-26

## Context
Saves must be backed up safely and the system must survive losing its DB.

## Decision
- Each backup = a ZIP snapshot of one save location + a **sidecar manifest** (game, location, files, hashes, trigger).
- The **disk is the source of truth**; the SQLite DB is a cache.
- **Reconcile** re-imports orphan ZIPs, recreates rows from manifests, prunes dead rows. Runs on startup + on demand (`cmd/gs-reconcile`).
- Restore takes a `preRestore` auto-backup first; retention keeps last N per location.

## Consequences
- Losing `gamesaver.db` is recoverable (scan + reconcile).
- Restore never destroys the current save irreversibly.
- See rules/backup-restore.md for the data-safety red lines.
