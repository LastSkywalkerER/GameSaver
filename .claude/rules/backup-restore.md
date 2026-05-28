# Rule — Backup, restore & reconcile

`internal/backup`. This is the part where a bug means **lost user save data**, so the rules are strict.

## Snapshots

- A backup = a ZIP of one save location, written under `<backupRoot>\<game-slug>\<timestampZ>__<hash>.zip`, plus a **sidecar manifest** describing what's inside (game, location, file list, hashes, sizes, trigger).
- `trigger` ∈ `manual | auto | preRestore | preMigrate`.
- Snapshots are listed newest-first (`created_at DESC`) — `snapshots[0]` is the latest everywhere in the UI.
- **Retention:** keep last N per save location (configurable; 0 = unlimited). Old snapshots beyond N are pruned **after** each new successful backup, never before.

## 🔴 Restore always auto-backups first

Before overwriting a live save, take a `preRestore` snapshot of the current state. Overwriting without
it = irreversible data loss. The restore API takes a "backup current first" flag and the UI passes true.

## Reconcile (DB ↔ disk)

- The DB is a **cache**, not the source of truth. The ZIPs + sidecar manifests on disk are.
- `Reconcile` (run on startup in the background, and via Settings button / `cmd/gs-reconcile`) re-imports orphan ZIPs that have a manifest but no DB row, can recreate missing game/location rows from manifests, and prunes dead rows pointing at deleted files.
- This is why losing `gamesaver.db` is recoverable: rescan + reconcile rebuilds it from disk.

## Data-safety red lines

- 🔴 "clean up / remove / filter" is **never** permission to delete snapshots or saves. Default = keep.
- 🔴 Never delete a ZIP without its row, or a row without checking the ZIP — go through reconcile.
- Restores and migrations (copy + NTFS junction) must be atomic enough that a crash mid-operation leaves either the old or the new state, never a half-written save.
