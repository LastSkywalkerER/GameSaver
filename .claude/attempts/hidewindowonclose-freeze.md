# Dead end — relying on Wails `HideWindowOnClose: true`

## Tried
Set `HideWindowOnClose: true` in the Wails options to make the X button minimize to tray.

## Why it failed
Clicking X froze the app for 1–2 s before hiding. WebView2 runs the full window-close path (teardown
hooks, cache flush) before Wails swaps in the hide, and that stutters visibly.

## What works instead
Hook `OnBeforeClose`, call `runtime.WindowHide(ctx)` ourselves, return true to cancel the real close.
Instant. In shell mode return false (X = real exit). See decision 0010.
