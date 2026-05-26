# GameSaver

Native Windows desktop app to **find, back up and restore game saves** across all your installed games and launchers — Steam, GOG, Epic, EA, Ubisoft, Battle.net, Xbox, Minecraft variants, and standalone / pirated copies (Goldberg, FLT, repacks).

Built on Go + Wails v2 + React/TS + Tailwind, with SQLite for state and the [Ludusavi manifest](https://github.com/mtkennerly/ludusavi-manifest) (52 000+ games) embedded for save-path resolution.

## What it does

- **Scans** every fixed drive for installed games via:
  - 8 launcher sources (Steam `libraryfolders.vdf` / `appmanifest_*.acf`, GOG registry + Galaxy, Epic `.item` manifests, EA, Ubisoft, Battle.net, Xbox Gaming Services, Minecraft launchers)
  - Heuristic disk walk with exe scoring for standalone / pirated installs
  - Goldberg SteamEmu / FLT / repack detection
- **Matches** each game to its save folders using the bundled Ludusavi manifest, with path-existence scoring for ambiguous names (e.g. "Apex" → Apex Legends only if `Saved Games\Respawn\Apex` actually exists), alias resolution (Alan Wake 2 → Alan Wake II), tokenizer fixes (`40,000`, `DeathStranding`, `Stray v1.3.214`, apostrophes, diacritics) and folder-glob coalesce.
- **Reverse-scans** the whole manifest to surface saves for games that aren't installed (orphan cards, e.g. saves you imported from another PC).
- **Pirate-scans** AppData / Saved Games / Documents for non-Ludusavi save folders matching installed games (catches repack save paths the manifest doesn't know about).
- **Pulls covers** from SteamGridDB (with API key) and Steam CDN (free).
- **Backs up** save folders as versioned ZIP snapshots with JSON sidecar manifests in a folder of your choice. Content-hash dedup, retention policy, file-coalesce guards against backing up entire game install dirs.
- **Reconciles** the backup folder on startup so the DB knows about every ZIP on disk, even after a DB wipe / reinstall.
- **Restores** any snapshot back to its original location (with a safety pre-snapshot of the current state).
- **Launches** games via deep-links (`steam://`, `com.epicgames.launcher://`, `goggalaxy://`, `origin2://`, `uplay://`, `battlenet://`) with fallback to the exe.
- **Manual override**: re-pick a game's Ludusavi entry, add custom save folders, run a per-game deep scan.

## Build

Requires Go 1.23+, Node 18+, [Wails CLI](https://wails.io/docs/gettingstarted/installation), and a C compiler (Windows: MinGW-w64 UCRT works fine).

```powershell
git clone git@github.com:LastSkywalkerER/GameSaver.git
cd GameSaver
wails build          # → build/bin/GameSaver.exe (≈32 MB)
```

For development:

```powershell
wails dev
```

Helper CLIs (in `cmd/`):

- `gs-smoke` — runs the full scan pipeline once and prints a report (without launching the UI).
- `gs-reconcile` — runs only the backup-reconcile pass.

## Runtime state

| What | Where |
|---|---|
| SQLite DB | `%LOCALAPPDATA%\GameSaver\gamesaver.db` |
| Settings (incl. SteamGridDB key) | `%APPDATA%\GameSaver\settings.json` |
| Cover cache | `%LOCALAPPDATA%\GameSaver\cache\covers\` |
| Logs (slog JSON) | `%LOCALAPPDATA%\GameSaver\logs\gamesaver.log` |
| Backup archives | user-chosen folder (default `C:\GameSavesBackup\`) |

Backups consist of `<slug>\<isoTimestamp>__<hash>.zip` + a sibling `.json` manifest with file list, hashes and game metadata — enough to fully reconstruct the DB if it's lost (see `cmd/gs-reconcile`).

## License

MIT
