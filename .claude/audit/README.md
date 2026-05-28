# Audit — doc/code drift notes

Record places where the docs (`CLAUDE.md`, `.claude/rules/*`) might have drifted from the code, when you
can't fix them immediately.

## Manual drift check (no script yet)
- §3 module map in `CLAUDE.md` ↔ `ls internal/` — adding/removing a package must update both.
- §5 stack table ↔ `go.mod` direct requires.
- decisions/ count ↔ the index list in `CLAUDE.md` §1.
- Latest release in `CLAUDE.md` header ↔ `git tag | sort -V | tail -1`.

## Open drift
- _(none recorded yet — this tree was authored fresh on 2026-05-28 to match the v0.7.8 code.)_

> A `make claude-audit` / script could automate the link + structure checks later; for now it's a manual eyeball before a docs commit.
