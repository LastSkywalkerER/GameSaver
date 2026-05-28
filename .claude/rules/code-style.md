# Rule — Code style

## Go

- Standard `gofmt`. `log/slog` for logging (structured, key/values). No `fmt.Println` debugging left in.
- Win32-touching code goes in `*_windows.go`. Keep syscall plumbing in a dedicated package (`controller`, `display`, `power`, `winutil`, `autologin`, `shellmode`) rather than inline in `app.go`.
- `app.go` is the Wails facade: thin methods that delegate to packages. Exported methods become the frontend API — name them clearly and keep payloads JSON-friendly (struct with json tags, or `map[string]any`).
- Background work runs in goroutines started from `Startup`; guard against `a.ctx == nil`.

## TypeScript / React

- Function components + hooks. Effects that subscribe must clean up (unsub the `EventsOn` handle, clear timers, remove listeners).
- Prefer explicit index passing over reading state in a closure (see the stale-closure rule in [`frontend-ui.md`](frontend-ui.md)).
- Tailwind utility classes; shared primitives (`.btn`, `.card`, `.input`, `.chip`) live in `style.css` `@layer components`.

## Comments — explain the "why", not the "what"

This codebase is full of non-obvious values that each encode an incident. **When you write or touch
one, leave a comment explaining the reason**, e.g.:

- the 30 s monitor-pick settling window,
- the 20 s sleep-suppress window,
- the 6 s playtime poll,
- the coalesce 100 MB / 3× thresholds,
- "poll all 4 XInput slots",
- "don't add padding to devModeW".

A future reader (human or Claude) must be able to tell *why* a magic number is what it is without
re-deriving the incident. This is the single most valuable convention in the repo.

## Commits

- Subject ≤ ~70 chars, imperative-ish. Body explains the *why* and the incident being fixed.
- One logical change per commit/tag. Co-author trailer as configured.
