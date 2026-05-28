# 0020 — Prefer exe over deep-link for non-Steam launches

Status: accepted
Date: 2026-05-28 (v0.7.8)

## Context
GOG games failed to launch with a "goggalaxy" protocol error: we preferred the stored `goggalaxy://`
LaunchURI, GOG Galaxy wasn't installed, and `cmd /c start` reported success for the unregistered scheme
(then the shell popped its own error), so the exe fallback never ran.

## Decision
- **Steam** prefers `steam://` (overlay/DRM/cloud), if the protocol is registered.
- **Everyone else** prefers the **bare exe** (what a desktop shortcut uses); deep-link only as fallback when there's no usable exe.
- Before using any deep-link, check `HKCR\<scheme>` for the `URL Protocol` value (`protocolRegistered`).

## Consequences
- GOG/DRM-free and most other games launch reliably via exe.
- We never fire an unregistered protocol and get a dead-end shell dialog.
- See rules/launcher.md (🔴).
