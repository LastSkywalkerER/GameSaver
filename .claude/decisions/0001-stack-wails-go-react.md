# 0001 — Stack: Wails v2 + Go + React/TS

Status: accepted
Date: 2026-05-26

## Context
Need a native Windows app: scan all drives, read launcher data, do filesystem work, talk to Win32, and
also present a rich UI with game cover art. Wanted one language for system work (Go) and a modern UI
without shipping a whole browser.

## Decision
**Wails v2** (v2.12.0): Go backend + WebView2-hosted React/TypeScript/Tailwind frontend. Backend
exposes methods via auto-generated bindings; communicates state via runtime events.

## Consequences
- Go does all the heavy lifting (scanning, zip, syscalls); React just renders.
- No Electron-sized binary; uses the OS WebView2.
- Frontend can't touch the FS/DB directly — everything routes through `app.go`. This is a feature (single mutation path) but means every capability needs a bound method.
- Wails events are fire-and-forget with no replay → late subscribers need getter methods (see decision 0013, rules/frontend-ui.md).
