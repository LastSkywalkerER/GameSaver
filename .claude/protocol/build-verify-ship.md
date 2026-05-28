# Protocol — Build, verify, ship

The one loop that governs every change to GameSaver. There is no staging, no installer; the user runs
released builds. So "done" has a precise meaning.

## The loop

```
1. Edit code.
2. wails build   →  must exit 0.            (local proof it compiles + TS typechecks)
3. git add <specific files> ; git commit    (why + incident in the body)
4. git push origin main
5. git tag vX.Y.Z ; git push origin vX.Y.Z
6. Watch the CI run to conclusion.          (Monitor tool, poll the run id)
7. Verify the release published.            (curl the releases/tags/vX.Y.Z API)
8. Only now tell the user it's shipped, with the release URL.
```

## Step 2 — local build (mandatory)

```bash
cd /e/claude/GameSaver && export PATH="/c/Program Files/Go/bin:$PATH" \
  && /c/Users/Администратор/go/bin/wails.exe build 2>&1 | tail -8; echo "build: $?"
```
Green = `Built '...GameSaver.exe'` + `build: 0`. Never push without this.

## Step 6 — watch CI

- Get the run id: `curl -s ".../actions/runs?per_page=1"` → grab `id` for `head_branch == vX.Y.Z`.
- Use the **Monitor** tool with a poll loop on that run id; it notifies on each status change and exits on `completed`. Do **not** sleep-poll manually.
- A run takes ~3–8 min. While waiting, don't claim success.

## Step 7 — verify release

```bash
curl -s ".../releases/tags/vX.Y.Z" | grep -m1 '"tag_name"'
```
For shell-mode-relevant releases, also confirm the `gamesaver-watchdog-windows-amd64.exe` asset exists.

## Task tracking

- Create one task per shippable unit (`TaskCreate`), mark `in_progress` when you start, `completed` only after step 7.
- The task title should name the feature + target version, e.g. "Settling window after monitor pick (v0.7.2)".

## Why so strict

The recurring failure mode in this project is shipping a fix that *compiles* but re-introduces a
self-inflicted loop (picker re-opens, sleep wakes, controller acts in background). Local build catches
syntax; CI catches the cross-platform build; only the user catches behavior. So: ship small, ship
tagged, and write the *why* down so the next regression is one read away.
