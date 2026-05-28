# 0004 — Path-scored matcher + alias/canonical naming

Status: accepted
Date: 2026-05-26

## Context
Naive name→manifest matching produced wrong covers, wrong AppIDs, duplicate cards (Alan Wake 2 vs II),
and orphan cards (Apex). Game names are ambiguous.

## Decision
- **Path-scored matching:** prefer manifest entries whose resolved save paths actually exist / overlap the install; reject name-only matches with no `Files`.
- **Alias map** (`aliasOf`) for renames; `matchGame` returns the canonical key; reverse scan skips aliases.
- **Tokenizer** splits CamelCase before lowercasing.

## Consequences
- Far fewer false matches and duplicates.
- The scoring thresholds and the coalesce safety limits (100 MB / 3×) are tuned to specific games — treat them as load-bearing constants (rules/save-matching.md), don't "simplify".
- Manual override exists for the cases the heuristic still gets wrong.
