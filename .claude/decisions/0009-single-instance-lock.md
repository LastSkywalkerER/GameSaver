# 0009 — Single-instance lock

Status: accepted
Date: 2026-05-27 (v0.3.7)

## Context
A second launch (double-clicking the tray-resident exe, or auto-update relaunch) spun up a second
playtime tracker, watcher, and tray → duplicate play sessions and unpredictable backups.

## Decision
Use Wails `options.SingleInstanceLock` (named mutex). A second exec hands off to the running instance
(which unhides + raises its window via an always-on-top toggle) and exits before `Startup` runs.

## Consequences
- No more duplicate background workers / sessions.
- The hand-off callback is the standard "bring existing window forward" pattern reused later for RestoreSelf.
