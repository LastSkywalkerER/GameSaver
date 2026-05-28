# 0007 — Auto-update via GitHub Releases (portable zip, no signing)

Status: accepted
Date: 2026-05-27

## Context
Solo project, no budget for code-signing certs, want painless updates.

## Decision
- Distribute as a **portable `.zip`** via GitHub Releases. **No installer, no code-signing.**
- CI builds + publishes on tag push (`v*`).
- In-app self-updater (`minio/selfupdate`): checks the GitHub API, prompts the user (banner), downloads, SHA-256 verifies, atomic exe swap, cleans up the `.old` rollback file on next launch.
- Background check at startup + every 30 min; manual "check now" emits the same banner.

## Consequences
- Zero distribution cost; updates land fast.
- Unsigned binary → SmartScreen warnings possible for end users (accepted trade-off).
- `main.AppVersion=="dev"` disables the version check for local builds.
- The watchdog is a separate downloadable asset (decision 0012).
