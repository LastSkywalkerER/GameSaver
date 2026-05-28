# Rule — Secrets

## 🔴 The SteamGridDB key (and any secret) never touches the repo

The user handed a SteamGridDB API key in chat **once**. It must only ever exist in two places:

1. The **CI repo secret** `STEAMGRIDDB_KEY`, injected at build time via ldflags into
   `GameSaver/internal/meta.DefaultSteamGridDBKey`.
2. The **final `settings.json`** generated on the user's own machine at runtime (gitignored).

It must **NEVER** appear in:
- source code, comments, or default values committed to git;
- commit messages, branch names, PR/issue text;
- logs (`slog`), toasts, or error messages;
- any file under `E:\claude\` — **including gitignored** files like `settings.json` in the working tree, dev notes, or scratch files Claude writes.

If you need to reference it, refer to it as "the SteamGridDB key", never the value.

## Other secret hygiene

- **Never log secrets or credentials.** Mask or omit.
- The embedded Ludusavi manifest had hardcoded AWS GameLift creds in one entry (*Squids from Space*) → GitHub push protection blocked the push. `scripts/sanitize-manifest.sh` strips such creds; run it whenever the manifest is refreshed. Verify the manifest is clean before committing.
- `.gitignore` already excludes `.env`, `settings.json`, `*.log`. Don't add real creds to `.env.dev`-style files either.
- Before any push, if a secret-scanning block appears, **stop** — do not bypass with `--no-verify`; find and remove the secret.

## In the UI

- The Settings field for the SteamGridDB key is `type="password"` and the placeholder explains the CI-injected key is used by default. A personal key entered there is stored only in the local `settings.json` and overrides the built-in one (higher rate limit, isolated).
