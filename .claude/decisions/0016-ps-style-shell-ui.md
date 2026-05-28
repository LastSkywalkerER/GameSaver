# 0016 — PlayStation-style shell UI as a separate React tree

Status: accepted
Date: 2026-05-27 (v0.5.0)

## Context
Shell mode needs a TV/controller-first UI, totally different from the windowed dashboard.

## Decision
When `runningAsShell`, `App.tsx` renders `<ShellApp>` instead of the normal layout: animated
background, hero panel + horizontal carousel, corner icons (settings/backups/power/exit/sound),
controller + keyboard + mouse-wheel navigation, games sorted recent-played first.

## Consequences
- Two UIs, one codebase; the windowed UI is untouched by shell work.
- Overlays (details = the existing GameDrawer, settings/backups = existing pages in a Modal) are reused, keeping the surface small.
- Input gating between carousel / overlays / picker / power-menu must be explicit (rules/frontend-ui.md).
