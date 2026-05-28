# `.claude/` — how this folder is organized

This folder is the project's memory. The root [`CLAUDE.md`](../CLAUDE.md) is the entry point and the
red lines; everything detailed lives here.

| Folder | What goes in it | When to add |
|---|---|---|
| `rules/` | **Code rules** — how the code must be written (do/don't). | When a pattern must be enforced repo-wide. |
| `protocol/` | **Claude workflow** — how Claude operates (build/verify/ship, proof, reasoning). | Rarely; when the working method changes. |
| `decisions/` | **ADR-lite** — one architectural/product decision per file. | After any non-trivial decision. Never edit a decided ADR; supersede it with a new one. |
| `chronicles/` | **Milestones** — narrative of how the project reached its current state. | After a phase ships. |
| `handoffs/` | **Session summaries** — done / didn't work / next step. | When closing a meaningful session. |
| `attempts/` | **Dead ends** — approaches that were tried and failed, so we don't re-try them. | After abandoning an approach. |
| `audit/` | Doc-drift notes — places where the docs and code may have diverged. | When you spot drift you can't fix immediately. |

## Conventions

- **Decisions** are numbered `NNNN-slug.md` and immutable once accepted. To change a decision, write a new one that says "supersedes NNNN".
- **Chronicles / handoffs** are dated `YYYY-MM-DD-slug.md`.
- **Filenames are kebab-case.**
- Link generously between files; the root `CLAUDE.md` should be reachable from any rabbit-hole in ≤2 hops.

## The one workflow rule that matters most

GameSaver has no staging branch and no installer. **Every change reaches the user as a tagged GitHub
release built by CI.** A change is not "done" until:

1. `wails build` exits 0 locally, AND
2. the `vX.Y.Z` tag's CI run concluded `success`, AND
3. the release published (zip + watchdog + checksums).

See [`protocol/build-verify-ship.md`](protocol/build-verify-ship.md).
