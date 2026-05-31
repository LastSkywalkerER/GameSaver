# 0025 — Shell controller overhaul + Settings as page

**Status:** accepted · 2026-05-31 · ships in v0.10.0

## Context

The v0.9.x shell-mode layout grew organically: A launched, Y opened details, X opened power,
Start opened settings, Back opened backups, plus separate 🖥 and 🎧 corner icons for monitor and
audio. Settings was rendered inside a `<Modal>` — a small dark card the gamepad couldn't drive any
of the controls in.

The user's brief was a complete pass:

1. Rule #1 — every UI surface must be reachable from the gamepad.
2. Rebind: Start = power, Y = settings, X = details, Back = devices chooser, plus d-pad up/down
   between the carousel and the corner-icons row.
3. Power menu gets Restart (and loses Сменить монитор → moves to the chooser).
4. Devices chooser (monitor / audio / Bluetooth) replaces the two separate corner icons.
5. Settings becomes a real full-screen page with the animated purple-blob background, controllable
   from the gamepad.
6. Backups stays on a corner icon, no controller face button.
7. All modals across normal + shell mode share the PowerMenu-style translucent-tiles look.
8. Bluetooth picker in Settings (deferred to v0.10.1).
9. Monitor button in Settings.
10. Wider Backups modal.
11. Softer ambient pad.
12. Themed checkboxes / radios.

## Decision

### Controller layout (`ShellApp.tsx`)
- A — launch active tile when focus is on the carousel; activate the focused corner icon when focus
  is on the icon row.
- Y — open Settings (full-screen page).
- X — open Game Details drawer.
- Start — open Power menu.
- Back / Select — open Devices chooser.
- B — close whatever overlay is on top; or, if focus is on the corner-icons row, drop focus back to
  the carousel.
- d-pad ↑ on the carousel jumps focus up to the corner-icon row; d-pad ↓ on the icons returns to
  the carousel. Within the icon row, ←/→ walks across them; A fires the focused one.

Keyboard mirror: ↑/↓ ↔ focus jump, ←/→ ↔ carousel/icon walk, Enter ≡ A, Esc ≡ B, i ≡ X, p ≡ Start,
d ≡ Back, , ≡ Y.

### Corner icons
Collapsed to 🔊 (sound toggle) · 🎛 (devices) · ⛁ (backups) · ⚙ (settings) · ⏻ (power). The
standalone 🛑 Exit Shell button is gone — Exit lives only inside the power menu where it belongs
beside Lock / Sleep / Reboot. The 🖥 and 🎧 icons are gone — replaced by the single 🎛 Devices
icon that pops a chooser.

### Power menu (`PowerMenu.tsx`)
Now Lock / Sleep / **Reboot** / Exit. Reboot delegates to a new
`App.RebootWorkstation` → `power.Reboot()` → `shutdown.exe /r /t 0` (we use the shell-out so apps
get a normal WM_QUERYENDSESSION cascade). It restores the multi-monitor layout BEFORE rebooting
so the user logs back into their original desktop. Reboot is double-confirmed via `confirmModal` —
Lock/Sleep are benign, Reboot is irreversible.

### Devices chooser (`DevicesMenu.tsx`)
PowerMenu-shaped modal: three tiles (Monitor / Audio / Bluetooth). On pick, closes itself and
delegates to ShellApp's existing pickers (MonitorPicker / AudioPicker). Bluetooth tile is a stub
showing "v0.10.1" in the description and emits a toast on press.

### Settings as a shell page (`ShellSettingsPage.tsx`)
Replaces the Modal wrapper. Full-screen, renders `<ShellBackground game={null}>` so the animated
blob background sits behind it, header strip with a Close button. Reuses the existing `SettingsPage`
component verbatim — but `SettingsPage` now accepts optional `onOpenMonitorPicker` /
`onOpenAudioPicker` props so the in-page "Монитор…" / "Аудио…" buttons can delegate to ShellApp's
picker host instead of falling back to system shortcuts.

Gamepad nav inside Settings:
- d-pad ↑/↓ walks every focusable in DOM order (`querySelectorAll('button, input, select, ...')`),
  calling `.focus()` so Tailwind's :focus-visible ring paints the active state in accent.
- A presses `.click()` on the focused control. Text/number/select inputs are skipped — A there
  would be a no-op; the user uses the touchpad or keyboard for fine-grained edits.
- B / Esc / Y closes back to the carousel (Y as the close shortcut is symmetric with Y opening
  Settings from the home screen).

### Modal style unification (`Modal.tsx`)
Base Modal updated to match the PowerMenu / DevicesMenu look: heavier black/80 backdrop with
backdrop-blur-md, body becomes a translucent `border border-white/10 bg-white/5` rounded-2xl
tile. Size prop (`md/lg/xl/2xl/4xl/6xl`) so data-heavy modals like Backups can request a wider
container — Backups now uses `6xl` to stop the Archive column from being chopped.

### Ambient pad softening (`sound.ts`)
The C3 root that made the v0.9 psstyle pad feel "давящий" is removed. Voicing is now the upper
structure (E3-G3-B3-D4) — bright and airy, no low-end weight. Master gain pulled to 0.011. Subtle
pack moves to G3+D4. Retro voiced an octave up at C4-E4-G4.

### Themed form controls (`style.css`)
`input[type=checkbox].gs-check` and `input[type=radio].gs-radio` zero out the native chrome
(`appearance: none`) and draw a square / circle in `bg-card border-border`. Checked state fills
with accent; checkmark/dot drawn via clip-path / pseudo-element scale animation. Applied
throughout Settings.

### Drawer touch-ups (`GameDrawer.tsx`)
`overflow-x: hidden` so long file paths can't make the whole drawer scroll sideways. Sessions row
gets `flex-wrap` so chips wrap on narrow widths instead of pushing the duration off the edge.
Gamepad: d-pad ↑/↓ scrolls the body by 180 px, B closes — per-row activation deferred to a future
pass (the per-row controls work fine via mouse / Tab).

## Consequences

- The home screen carousel + corner icons are now a single focus graph: every action is reachable
  via the d-pad from any focus point. Mouse and keyboard interactions still work unchanged.
- The 🖥 / 🎧 split is gone — one less mental model for the user. Bluetooth slots into the same
  chooser the moment v0.10.1 backs it.
- Settings is now usable from a couch with only the gamepad in hand: scroll-to-control, A to
  toggle / open, B to back out. Text inputs still want a keyboard, which is expected.
- Reboot from the power menu lets users restart Windows without dropping back to Explorer first —
  one of the last papercuts in shell-as-daily-driver use.
- The unified Modal look closes the visual gap between the PowerMenu family and confirm dialogs;
  everything now reads as the same app.

## Alternatives considered

- **Keep Settings as a Modal but with full gamepad nav**: tried and dropped — a modal with the
  background showing through reads as a temporary thing, but settings IS the place a user dwells
  in (sound packs, backup root, watcher debounce). A full page felt right.
- **Drop Restart entirely (Sleep covers most cases)**: the user explicitly asked for it, and the
  cost is one `shutdown.exe` shell-out. Worth the slot.
- **Render the desktop Modal markup unchanged in shell mode**: rejected — its `card` class is a
  solid panel that fights the translucent shell aesthetic. The new Modal styling fixes that for
  ALL surfaces at once.

## Deferred to v0.10.1

- Bluetooth picker (item #28 in the brief). Win32 BluetoothFindFirstDevice +
  BluetoothSetServiceState are not difficult per call but properly handling the async
  pairing / connecting lifecycle and surfacing it as a controller-friendly picker is its own
  decision document. The DevicesMenu tile is already in place — the picker just slots in behind
  it.
