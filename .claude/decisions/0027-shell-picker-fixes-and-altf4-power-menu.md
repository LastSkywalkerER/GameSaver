# 0027 — Shell-picker fixes + Alt+F4 → power menu

**Status:** accepted · 2026-06-07 · ships in v0.10.6

## Context

Three issues reported from the in-shell device pickers / kiosk UX:

1. **Audio picker doesn't switch.** Clicking any row in the AudioPicker closed the modal but the
   default output never changed.
2. **Bluetooth connect fails silently.** Selecting a paired headset showed a red toast
   `BT: SetServiceState 110B (errno 0): The operation completed successfully.` with no observable
   connection attempt.
3. **Alt+F4 kills the kiosk.** In shell mode Alt+F4 exited GameSaver (exit 0), so the watchdog
   launched Explorer — an easy, accidental way to fall out of the console UI. Wanted: Alt+F4 opens
   the same power menu as the Start button / ⏻ corner icon.

## Decision

### 1. AudioPicker — pass the clicked device explicitly

The click path did `setIdx(i)` then `applyChoice()`, but `applyChoice()` read `col[idx]` from its
render closure. `idx` is pre-seeded to the current default (so a single A confirms), so the stale
read returned the **default** device → `isDefault` was true → the handler did `playBack(); onDone()`
and closed without switching. Same stale-closure class as PowerMenu (v0.7.7), but nastier: the stale
value was the *no-op* item, so it presented as "nothing happens" instead of an obvious misfire.

Fix: split into `applyDevice(dev)` (acts on a device passed in) + `pickFromColumn(d)` for clicks,
which resolves the device from its own column list and never reads the focused `idx`. Controller /
keyboard still call `applyChoice()` → `applyDevice(col[idx])`. Documented in `rules/frontend-ui.md`.

### 2. Bluetooth — real radio handle + read the right error code

Two bugs in `internal/bluetooth` (`setServicesByID`):

- `BluetoothSetServiceState` was called with `hRadio = NULL`. Win10/11 reject that with
  `ERROR_INVALID_PARAMETER (87)`, so no service flipped and nothing connected. Fixed by opening the
  first local radio with `BluetoothFindFirstRadio` and passing its handle (fallback to NULL only if
  there is no radio); the radio `HANDLE` is released with `CloseHandle`, the find handle with
  `BluetoothFindRadioClose`. (`btFindRadioParams` was already declared but unused — the radio path
  had been stubbed out.)
- The error code is the function's **DWORD return value**, not `GetLastError`. We were formatting the
  3rd `proc.Call` return (GetLastError), which is `Errno(0)` on this no-SetLastError path — hence the
  nonsensical "errno 0: The operation completed successfully" on a real failure. Now we format `r`
  (`syscall.Errno(r)`); `1060`/`1168` stay clean skips.

Documented in `rules/windows-syscalls.md`.

### 3. Alt+F4 → power menu (close-intercept + `quitRequested` flag)

`main.go` `OnBeforeClose` in shell mode now emits `shell:open-power-menu` and returns **true**
(cancels the close) instead of returning false (exit). `ShellApp` subscribes and opens the PowerMenu
— the existing Lock / Sleep / Reboot / Exit grid. The intercept lives in Go because the OS delivers
Alt+F4 to the window before the WebView, so a JS keydown handler can't reliably catch it.

A deliberate quit must still work, so `App.quitRequested` (`atomic.Bool`) is set by `QuitApp()` before
`runtime.Quit`; `OnBeforeClose` checks it first and lets genuine quits through. The tray "Выйти" now
routes through `QuitApp()` (not `runtime.Quit` directly) so it sets the flag too. Normal (non-shell)
mode is unchanged (close → hide-to-tray). Updated `rules/shell-mode.md` (the old "X button is a real
exit" note no longer holds).

## Consequences

- The kiosk is much harder to fall out of by accident; the only deliberate exit is the power-menu
  Exit (or the watchdog escape hatches).
- All three shell-mode escape hatches are preserved (watchdog `Ctrl+Alt+Shift+F12`,
  `--disable-shell`, power-menu Exit). The power-menu Exit now depends on the `quitRequested` flag.
- No new Wails-bound methods (QuitApp already existed; `quitRequested` is an unexported field) → no
  binding regeneration needed.

## Risks / known gaps

- If a full-screen picker (audio/BT/volume/monitor, z-2000) is open when Alt+F4 is pressed, the
  PowerMenu (z-1500) opens behind it. The close is still cancelled (kiosk-safe) and the menu is
  visible once the picker closes. Not worth special-casing.
- The Bluetooth fix builds and passed the user's v0.10.6 shell test; headphone-specific profile
  quirks remain device-dependent, but a genuine hard failure now surfaces the **real** Win32 code in
  the toast instead of "errno 0", so the next failure is diagnosable.
