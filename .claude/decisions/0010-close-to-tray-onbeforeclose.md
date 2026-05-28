# 0010 — Close-to-tray via OnBeforeClose, not HideWindowOnClose

Status: accepted
Date: 2026-05-27 (v0.3.8)

## Context
With `HideWindowOnClose: true`, clicking X froze the app for 1–2 s before hiding (WebView2 runs the full
close path before swapping in the hide).

## Decision
Hook `OnBeforeClose`, call `WindowHide(ctx)` ourselves, and return true to cancel the real close. Same
end state, no freeze. (In shell mode, `OnBeforeClose` returns false — X is a real exit.)

## Consequences
- Instant hide-to-tray in normal mode.
- See attempts/hidewindowonclose-freeze.md.
