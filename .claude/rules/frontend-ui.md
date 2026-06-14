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

- Procedural Web Audio API nav tones (`playMove/playSelect/playBack`) + a looped MP3 ambient pad (`startAmbient/stopAmbient`). Since v0.10.1 the sound-pack picker is gone — one knob, `gs:soundOn` (boolean), pinned to the "subtle" voice (legacy `gs:soundPack` migrated on read). See `decisions/0026`.
- **All output routes through one master `GainNode`.** `setMuted(true/false)` ducks/raises it to silence the *whole* launcher at once. 🔴 This is how audio is killed while the shell is minimized behind a running game — driven explicitly from `doLaunch`/`playtime:changed`, **not** from `visibilitychange` (WebView2 doesn't fire it on OS-minimize, so the old `onVis → stopAmbient` was dead for that case). See `decisions/0029`. Don't route new sounds straight to `ctx.destination` — go through `master(a)` so mute covers them.

## Modals

- Use `<Modal>` / `confirmModal()` (themed, blurred backdrop, ESC/backdrop-close, Enter confirms) — **never** `window.confirm()` (it pops the ugly "wails.localhost" OS dialog).

## Stale-closure gotcha (recurring bug)

- A click handler that does `setActive(i)` then calls a `confirm()` which reads `active` from its closure fires the **previously selected** item, not the clicked one. **Pass the index explicitly** (`activate(i)`), and add `onMouseEnter={() => setActive(i)}` so hover tracks the cursor. Incident: PowerMenu (v0.7.7).
- **Recurred in AudioPicker (v0.10.6).** The click did `setIdx(i)` then `applyChoice()`, which read `col[idx]` from the stale closure. Because `idx` is pre-seeded to the current default (so one A confirms), every click read the **default** device, saw `isDefault`, and just `playBack()+onDone()` — closing the modal without switching. Fixed by passing the device object explicitly (`applyDevice(dev)` / `pickFromColumn`). ⚠️ When a list pre-selects a default, the stale read isn't just "wrong item" — it can be the **no-op** item, so the bug hides as "nothing happened" rather than an obvious misfire. See `decisions/0027`.

## Persisted UI prefs (localStorage keys)

- `gs:sortBy` (dashboard sort; migrate old `saveSize` → `installSize`)
- `gs:sidebarCollapsed`, `gs:tilePrefs`, `gs:soundPack`, `gs:soleMonitorId`, `gs:navSoundOn`

## i18n (`i18n.ts`)

- ru/en. Default = **system locale** (`navigator.language`) until the user explicitly picks one in Settings; backend `Language` defaults to empty string to signal "no choice yet".
