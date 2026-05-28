# 0002 — SQLite via modernc.org/sqlite (no cgo for the driver)

Status: accepted
Date: 2026-05-26

## Context
Need embedded persistence for games/installs/saves/snapshots/sessions. `mattn/go-sqlite3` needs cgo +
a C toolchain, which complicates the build.

## Decision
Use **modernc.org/sqlite** — a pure-Go SQLite. The DB driver itself requires no cgo.

## Consequences
- Simpler builds; fewer toolchain surprises for the DB layer.
- (Note: the overall app/CI still builds with CGO_ENABLED=1 for the Wails/WebView side; that's separate from the SQLite driver choice.)
- Schema is managed with idempotent `addColumnIfMissing` migrations (rules/storage-sqlite.md). The DB is a cache, rebuildable from disk via reconcile (decision 0006).
