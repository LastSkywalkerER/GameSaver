# Rule — Frontend / UI

React 18 + TypeScript + Vite + Tailwind, bridged to Go by Wails. `frontend/src/`.

## Backend bridge

- Call backend via `api.*` (re-exported generated bindings from `frontend/wailsjs/`). Subscribe to backend events via `EventsOn("<event>", cb)`; the returned function unsubscribes — store it and call it in the effect cleanup.
- 🔴 **Wails does not replay missed events.** The backend may emit `controller:state`/`update:available`/etc. before the frontend has subscribed. For any state a late-mounting component needs, the backend must also expose a **getter** (`IsControllerConnected`, `GetShellModeStatus`, `GetConfig`) and the component pulls it on mount.
- All persistence/mutation goes through `app.go`. The frontend never writes files or the DB.

## Two UIs

- `App.tsx` branches on `runningAsShell` (from `GetShellModeStatus`). Normal mode = sidebar + topbar + pages. Shell mode = `<ShellApp>` (immersive). While `shellMode === null` (still polling) render nothing to avoid a flash of the wrong UI.
- Global controller handlers in `App.tsx` short-circuit in shell mode so they don't double-handle with `ShellApp`'s own bindings.

## Controller input (`controller.ts`)

- Hooks: `useControllerConnected()`, `useControllerNav(handler)`, `useControllerButton(handler)`.
- Only **one** active component-tree should react at a time. Gate handlers with flags (`pickerOpen`, `inputBlocked`, overlay state) so e.g. the monitor picker owns the controller while it's up and the carousel behind it doesn't move.
- A shared `moveCursor` helper centralizes "play the move sound only when the cursor actually shifts".

## Sound (`sound.ts`)

- Procedural Web Audio API tones — **no audio asset files**. Packs: `psstyle` (default), `subtle`, `retro`, `off`. Persisted in localStorage. `playMove/playSelect/playBack`.

## Modals

- Use `<Modal>` / `confirmModal()` (themed, blurred backdrop, ESC/backdrop-close, Enter confirms) — **never** `window.confirm()` (it pops the ugly "wails.localhost" OS dialog).

## Stale-closure gotcha (recurring bug)

- A click handler that does `setActive(i)` then calls a `confirm()` which reads `active` from its closure fires the **previously selected** item, not the clicked one. **Pass the index explicitly** (`activate(i)`), and add `onMouseEnter={() => setActive(i)}` so hover tracks the cursor. Incident: PowerMenu (v0.7.7).

## Persisted UI prefs (localStorage keys)

- `gs:sortBy` (dashboard sort; migrate old `saveSize` → `installSize`)
- `gs:sidebarCollapsed`, `gs:tilePrefs`, `gs:soundPack`, `gs:soleMonitorId`, `gs:navSoundOn`

## i18n (`i18n.ts`)

- ru/en. Default = **system locale** (`navigator.language`) until the user explicitly picks one in Settings; backend `Language` defaults to empty string to signal "no choice yet".
