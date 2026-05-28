# 0015 — Monitor picker spans the virtual desktop; settling/suppression windows

Status: accepted
Date: 2026-05-28 (v0.7.1–v0.7.3)

## Context
After sleep, a monitor can stay "attached" in the topology but go physically dark. Silently re-applying
the remembered monitor landed the UI on the dead screen with no way out. Also, disabling monitors emits
`display:changed`, which naively reopened the picker forever / aborted sleep.

## Decision
- On every shell launch and on `display:changed`, re-enable all monitors and **span our window across the whole virtual desktop**; render one self-identifying picker copy per physical monitor ("📍 этот экран"). Whatever screen is lit shows a usable picker.
- **Settling window (30 s):** after a pick, `display:changed` silently re-asserts the chosen monitor instead of reopening (debounced 3 s).
- **Suppression window (20 s, wall-clock):** before Lock/Sleep, ignore `display:changed` so the power-down churn doesn't reopen the picker or abort the sleep; a real wake hours later is past the window and shows the picker.

## Consequences
- No more "stuck on a dark monitor" and no more picker/sleep loops.
- Mixed-DPI multi-monitor layouts may be slightly misaligned (one webview, one devicePixelRatio) — acceptable for same-DPI gaming setups.
- These windows are load-bearing magic numbers (rules/shell-mode.md, protocol/structured-reasoning.md).
