# 0026 — Bluetooth picker, right-stick scroll, MP3 ambient loop

**Status:** accepted · 2026-05-31 · ships in v0.10.1

## Context

User feedback on the v0.10.0 shell:

1. B doesn't close the Backups modal — only Esc on the keyboard worked.
2. Settings is "reachable" via gamepad but the focus isn't visible enough; left stick and d-pad
   should both walk the controls, and the right stick should scroll the page.
3. Same gripe inside the GameDrawer — focus moves but invisibly.
4. Drop the sound-pack picker; keep only the "тихий" voice.
5. Replace the procedural ambient drone with a real audio file (the user attached
   `universfield-melancholic-ambient-background-351787.mp3`), trim the first/last 15 s, loop with a
   seamless crossfade.
6. Ship the Bluetooth picker that the v0.10.0 DevicesMenu had as a placeholder.

## Decision

### 1. B closes Backups (`ShellApp.tsx`)
The Backups overlay is the one shell-mode surface that uses the base `<Modal>` (which only listens
for keyboard Esc). All other overlays (PowerMenu, DevicesMenu, GameDrawer, ShellUpdateModal,
ShellSettingsPage) wire their own B handler. We add a single special case in ShellApp's
controller-button hook: if `overlay === "backups"` and the press is B/Back, close. Targeted fix —
not worth a full refactor of base `<Modal>` to subscribe to the controller bus.

### 2 + 3. Right-stick scroll + visible focus

Backend `internal/controller/xinput_windows.go` now reads `ThumbRX/ThumbRY` each 20 ms tick and
emits a new event:

```
controller:scroll  {dx: number, dy: number}   // both normalised [-1.0..+1.0]
```

`dy` is flipped to be DOM-positive (scroll down = +) so frontend can hand it straight to
`el.scrollBy({ top: dy * speed })`. Smaller deadzone than the d-pad nav (`8000` vs `16000`) — the
right stick is doing fine-grained scrolling, not coarse menu navigation. Idle ticks emit nothing.

Frontend `controller.ts` exposes `useControllerScroll(handler)`. Used by `ShellSettingsPage` and
`GameDrawer` with a px factor of 18 (≈ 900 px/s at full deflection — matches a continuous wheel
scroll).

For visible focus, `style.css` now has a universal `*:focus-visible` rule that paints a 2px accent
outline with offset, applied to every focusable. We previously relied on per-component focus
styling which was inconsistent (buttons had no ring). Custom checkboxes / radios keep their own
box-shadow ring (more appropriate for round / square indicators).

GameDrawer adopts the same roving-focus model as ShellSettingsPage:
- d-pad / LS up/down (AND left/right — drawer is single-column, so the horizontal axis would be
  silent otherwise) walks every focusable in DOM order.
- A presses `.click()` on the focused control. Inputs / selects skipped (A there would be a no-op).
- Right stick scrolls the body.
- B closes.

### 4. Sound — pin "subtle", drop pack picker

`sound.ts` simplified to one knob: `gs:soundOn` (boolean). Nav tones are hardcoded to the v0.9
"subtle" voice (short sines, ~0.05–0.07 gain). The `SoundPack` type and `getSoundPack/setSoundPack/
subscribeSoundPack` exports are kept as backward-compatible shims that map to the boolean, so
existing callers (CornerIcons sound toggle, Settings checkbox) don't need to change shape at the
same time as the ambient swap. Settings now shows a single "Включить звук" checkbox plus the three
preview buttons (Move / Select / Back).

Legacy `gs:soundPack` localStorage key is migrated on first read: anything other than literal
`"off"` becomes `true`.

### 5. MP3 ambient with seamless loop

The user-supplied MP3 is copied verbatim into `frontend/src/assets/ambient.mp3`. Vite bundles it
into the build at a fingerprinted URL. At runtime (sound.ts):

1. `fetch(ambientMp3)` + `decodeAudioData` → an AudioBuffer.
2. Trim head 15 s + tail 15 s by sample-counting against the buffer's sampleRate.
3. Bake a seamlessly-looping output buffer of length `(trimmed - W)` samples, where `W` is a 6 s
   crossfade region. For `i ∈ [0, W)`:
     `out[i] = original_head[i] · sin(t · π/2) + original_tail[i] · cos(t · π/2)`
     where `t = i / W`.
   For `i ∈ [W, trimmedLen - W)`: `out[i] = original_head[i]`.
   Sin² + cos² = 1 keeps perceived loudness flat through the blend. When `source.loop = true` the
   buffer's last sample seamlessly continues the original timeline at the start.
4. `source.start()` with `loop: true`. Master gain ramps 0 → 0.10 over 4 s (matches the procedural
   drone's old envelope so cold-start doesn't pop).

The decode + bake runs once per session, gated behind `loopBufferPromise` so repeated start/stop
toggles don't re-fetch the MP3.

### 6. Bluetooth picker

New `internal/bluetooth` package wraps the Win32 BluetoothAPIs.dll exports:

```
BluetoothFindFirstDevice / BluetoothFindNextDevice / BluetoothFindDeviceClose
BluetoothSetServiceState
```

Layout-critical struct (`BLUETOOTH_DEVICE_INFO`) is 560 bytes on x64 per MSDN — Go natural
alignment with explicit pad after `dwSize` matches that. `runtime.LockOSThread` while we hold a
find-handle so the Go scheduler doesn't yank it.

"Connect" means enable every audio-related service GUID we know about (A2DP sink, AVRCP, HFP,
HSP). Different headphones surface themselves under different combinations and enabling an
unsupported one returns `ERROR_SERVICE_DOES_NOT_EXIST` which we treat as a clean skip. As long as
at least one service flipped successfully, the call returns nil. Errors are first-non-skip-error.

A `sync.Mutex` serialises the slow blocking calls — `BluetoothSetServiceState` can block up to
~30 s while the radio negotiates with the headset; queueing concurrent requests in the radio gives
chaotic error reporting otherwise.

Pairing is deliberately out of scope. The `OpenWindowsBluetoothSettings` shortcut punts to the
system add-device wizard for "I want to pair a new device".

> **Correction (v0.10.8, [decision 0028](0028-sysinternals-autologon-and-bt-pairing-wizard.md)):**
> this originally launched `fsquirt.exe`, called here "the Bluetooth add-device wizard". That was
> wrong — `fsquirt.exe` is the **Bluetooth File Transfer** wizard (send/receive files), so the button
> dropped users into a file-transfer dialog. Fixed to launch `DevicePairingWizard.exe` (the real
> "Add a device" wizard).

`App.ListBluetoothDevices` / `ConnectBluetoothDevice` / `DisconnectBluetoothDevice` /
`OpenWindowsBluetoothSettings` exposed via Wails.

Frontend `BluetoothPicker.tsx`: full-screen list-style modal (like AudioPicker, but vertical
instead of two-column). Sort: audio devices first, then alphabetical. A toggles connect /
disconnect, the in-flight row shows a spinner pill. X / R refreshes. Pair-new-device button at
the bottom opens the system applet.

Wired into:
- DevicesMenu (BT tile no longer disabled; clicks open BluetoothPicker).
- Settings page → 📶 Bluetooth button delegates via the new `onOpenBluetoothPicker` prop (same
  shape as Monitor / Audio).

## Consequences

- The DevicesMenu now covers every "what's connected to me" surface a controller user might want.
- Right-stick scroll is a new general primitive — future scrollable surfaces just call
  `useControllerScroll`.
- Ambient is no longer purely procedural (the `frontend-ui.md` "no audio asset files" rule needs
  an update — see the corresponding edit). The asset is bundled, not downloaded, so the
  no-asset-files rule was about "no audio assets to ship separately"; we're still self-contained.
- BT support has the inherent limitation that pairing happens in Windows Settings — a future pass
  could add in-app pairing via `BluetoothAuthenticateDeviceEx`, but the PIN-and-inquiry workflow
  is a substantial UX detour and isn't in this tag.

## Risks / known gaps

- The blocking `BluetoothSetServiceState` call holds the Wails thread for up to 30 s. The UI is
  responsive (Wails marshals each method on its own goroutine) but the user sees a long spinner.
  Mitigations: timeout could be added inside the loop, but Windows itself controls the negotiation
  duration. Accept for now.
- `ulClassOfDevice == 0` for some devices (Windows can't always tell), so the "audio" filter is
  an inexact hint, not a hard gate. The picker still LISTS non-audio paired devices; it just sorts
  them after the audio ones.
- Embedded MP3 is ~4 MB → noticeable bundle growth. Acceptable for a desktop installer.
