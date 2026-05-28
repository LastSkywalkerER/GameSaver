# Rule — SQLite storage

`internal/storage/sqlite`, driver **modernc.org/sqlite** (pure Go, the driver needs no cgo — see
[`../decisions/0002-sqlite-modernc-cgo-free.md`](../decisions/0002-sqlite-modernc-cgo-free.md)).

## Migrations are idempotent and forward-only

- Schema lives behind a `schema_version` plus idempotent `ALTER`s applied via `addColumnIfMissing(db, table, col, def)` which checks `PRAGMA table_info` first.
- New columns are added to the `extraColumns` table in `migrations.go`, e.g.:
  ```go
  {"installations", "install_dir_size_bytes", "INTEGER NOT NULL DEFAULT 0"},
  ```
- 🔴 **Never modify a migration that has shipped.** Old DBs in the wild must still open and upgrade. Add a new idempotent step instead.
- Every added column needs a sane `DEFAULT` so existing rows are valid.

## Conventions

- The DB lives in the user data dir (`%LOCALAPPDATA%\GameSaver\`). It is a **cache** — losing it is recoverable via scan + reconcile (see [`backup-restore.md`](backup-restore.md)).
- Snapshots are returned `created_at DESC`. Keep that ordering contract — the UI assumes `[0]` is newest.
- Select column lists are centralized (e.g. `installSelectCols`) so a new column is added in one place and all scanners stay in sync.
- Timestamps are unix seconds (`int64`), `0` meaning "never".
