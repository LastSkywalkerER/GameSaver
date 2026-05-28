# 0008 — SteamGridDB key injected via CI secret (never in source)

Status: accepted
Date: 2026-05-27

## Context
Cover/hero art comes from SteamGridDB, which needs an API key. The key must not live in the repo.

## Decision
- Ship a default key by injecting the CI repo secret `STEAMGRIDDB_KEY` at build time via ldflags into `internal/meta.DefaultSteamGridDBKey`.
- Users may enter their own key in Settings (stored only in the local `settings.json`), which overrides the built-in one.

## Consequences
- The key is never in git, logs, or any working-tree file. See rules/secrets.md (🔴 red line).
- If the CI secret is unset, the binary ships with an empty default and falls back to per-user input.
- A personal key gives a higher rate limit and isolation.
