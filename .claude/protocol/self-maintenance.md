# Protocol — Self-maintenance of `.claude/`

Keep the memory honest and current.

## When to write what

| Trigger | Artifact |
|---|---|
| A non-trivial architectural/product choice | `decisions/NNNN-slug.md` (ADR-lite). Immutable once accepted — supersede, don't edit. |
| Something broke / Claude did the wrong thing / user re-tested | A line in `CLAUDE.md` §6 Red Lines **or** the relevant `rules/*.md`, with the incident. If the approach itself was a dead end → `attempts/`. |
| A phase / big feature shipped | `chronicles/YYYY-MM-DD-slug.md` (narrative milestone). |
| Closing a meaningful session | `handoffs/YYYY-MM-DD-slug.md` (done / didn't work / next). |
| Tried an approach that failed | `attempts/slug.md` so nobody re-tries it. |

## ADR format (keep it short)

```
# NNNN — <title>
Status: accepted | superseded by NNNN
Date: YYYY-MM-DD
Context: <the forces / the problem>
Decision: <what we chose>
Consequences: <trade-offs, what it costs us>
```

## Handoff format

```
# YYYY-MM-DD — <slug>
Shipped: <tags/features that landed>
Didn't work / open: <bugs still open, user reports pending>
Next: <the obvious next step>
Watch out for: <traps for the next session>
```

## Drift

- The root `CLAUDE.md` §3 module map and §5 stack table must match `internal/` and `go.mod`. When you add a package or bump a dep, update them in the same change.
- If a rule and the code disagree, **stop and ask which is truth** — don't silently "fix" either to match.
- Record drift you can't fix immediately in `audit/`.
