# 0023 — In-app audio device picker (controller-driven)

**Status:** accepted · 2026-05-31 · ships in v0.9.1

## Context

Shell mode is controller-first: a user without keyboard/mouse must be able to do everything from the
pad. Audio device switching was the one hole — until now the only affordance was a Settings button
that opened the system "Параметры → Звук" page, which is mouse-only and dumps the user out of the
fullscreen shell. Same problem the monitor picker solved a year earlier.

## Decision

### `internal/audio` (pure-Go COM, no CGO)
A new Windows-only package enumerates active render (output) + capture (input) endpoints and switches
the default. Two layers:

- **MMDevice API** for list + current-default — documented (`IMMDeviceEnumerator`,
  `EnumAudioEndpoints`, `OpenPropertyStore`, `PKEY_Device_FriendlyName`).
- **IPolicyConfigVista** for `SetDefaultEndpoint` — the same **undocumented** COM interface every
  Windows audio switcher (SoundSwitch, NirSoft SoundVolumeView, AHK scripts) has ridden since Vista.
  CLSID `{870af99c-171d-4f9e-af0d-e63df40c2bc9}`, IID `{568b9108-44bf-40b4-9006-86afe5b5a620}`,
  `SetDefaultEndpoint` at **vtable slot 12** (the Vista variant has an extra
  `ResetDeviceFormat` and pushes it to 13 — wrong one for Win7+/10/11).

Hand-rolled vtable dispatch via `syscall.SyscallN`. Each Wails-bound call does
`CoInitializeEx(MTA) + LockOSThread` and unwinds on exit. Same pattern as `internal/display`.

`SetDefault` flips the default for **both `eConsole` and `eMultimedia`** roles — what the "Set
Default" button in the Windows Sound dialog does. `eCommunications` is left alone.

### Frontend `AudioPicker`
Full-screen modal, two columns (🔊 Вывод / 🎤 Ввод). Controller bindings mirror MonitorPicker:

- ↑↓ moves within a column · ← → / LB / RB swap columns · A applies · B / Esc closes.
- The current default in each column is pre-selected so a single A re-confirms (matches the
  monitor picker's "remembered choice = pre-selected" feel — see `decisions/0015`).
- Apply calls `SetDefaultAudioDevice(id)` and re-fetches; the badge moves to the new device without
  closing the modal, so the user can switch again immediately.

### Three entry points
1. **Settings page** — replaces the old "Открыть «Параметры → Звук»" button with a button that opens
   the in-app picker. System-settings shortcut is kept as a "last-resort" sibling button.
2. **🎧 corner icon** in shell mode, next to the 🖥 monitor switch.
3. **Auto-chain on shell startup**: after the MonitorPicker's `onDone` fires for the first time in
   the session (or after the picker is skipped because there's only one display), open the
   AudioPicker once. A `useRef` guard makes it strictly one-shot — re-picking a monitor from the
   corner icon mid-session does NOT drag the audio picker back up.

## Consequences

- Controller users get monitor + audio in one short flow on every shell launch.
- The undocumented IPolicyConfig is a known stability tradeoff: if Microsoft ever rotates the CLSID,
  `SetDefault` returns a clean HRESULT error and the rest of the picker (list + UI) keeps working.
  The same fallback every other audio switcher relies on.
- `inputBlocked` in `ShellApp` now also gates on `audioOpen`, so the carousel behind the picker
  doesn't double-handle d-pad/A.

## Alternatives considered

- **Shell out to `nircmd` / SoundSwitch CLI**: external binary, licensing, packaging cost. The COM
  layer is ~280 lines; not worth dragging a dependency in.
- **WMI / PowerShell**: WMI doesn't expose `SetDefault`; would still need IPolicyConfig under the
  hood, plus a `powershell.exe` spawn per call. Slower and uglier.
- **Open the system "Sound" panel only**: rejected — defeats the controller-first goal.
