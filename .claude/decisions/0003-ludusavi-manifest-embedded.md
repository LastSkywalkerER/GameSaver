# 0003 — Embed the Ludusavi manifest for save matching

Status: accepted
Date: 2026-05-26

## Context
Finding a game's real save folder is the core hard problem. The community-maintained **Ludusavi
manifest** maps 53k+ games to their save-path globs/hubs.

## Decision
Embed the manifest (`internal/match/data`, `go:embed`) and match games against it offline. No network
dependency at scan time.

## Consequences
- Works offline; deterministic.
- The manifest is ~17 MB YAML — embedded into the binary.
- Must be **sanitized** before embedding (one entry had hardcoded AWS creds → push-protection block). `scripts/sanitize-manifest.sh`, see rules/secrets.md.
- Refreshing the manifest is a deliberate step (re-sanitize, rebuild).
- Forward match alone misses unmatched games → complemented by reverse scan (decision 0005).
