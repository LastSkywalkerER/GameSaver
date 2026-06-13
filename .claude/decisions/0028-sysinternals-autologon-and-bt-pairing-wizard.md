# 0028 — Sysinternals Autologon button + fix BT "add device" (fsquirt → DevicePairingWizard)

**Status:** accepted · 2026-06-13 · ships in v0.10.8

## Context

Two user-reported items on the Settings / Bluetooth surfaces:

1. **Autologon:** the only passwordless-logon path was the netplwiz checkbox dance
   ([decision 0018](0018-autologin-via-netplwiz.md)). It's fragile — hidden on Win11 22H2+ (we
   unhide it via a reg tweak) and Microsoft keeps moving the user-accounts UI. The user asked for a
   second, more robust option using Microsoft's own
   [Sysinternals Autologon](https://learn.microsoft.com/sysinternals/downloads/autologon): download,
   open, configure.

2. **Bluetooth "add device":** the "➕ Запарить новое (Windows)" button in `BluetoothPicker` opened
   the wrong thing — a **file-transfer** dialog instead of device pairing. Root cause:
   `OpenWindowsBluetoothSettings` launched `fsquirt.exe`, which decision 0026 wrongly called "the
   Bluetooth add-device wizard". `fsquirt.exe` is the **Bluetooth File Transfer** wizard (send/
   receive files).

## Decision

### 1. Sysinternals Autologon (alternative to netplwiz)

New `internal/autologin/sysinternals_windows.go`:

- `EnsureAutologon(ctx)` downloads `https://download.sysinternals.com/files/AutoLogon.zip` (~0.5 MB)
  on first use and extracts the architecture-appropriate exe into
  `%LOCALAPPDATA%\GameSaver\bin\Autologon.exe` — same on-demand-binary pattern shellmode uses for
  the watchdog. The zip ships `Autologon.exe` (x86) / `Autologon64.exe` (x64) / `Autologon64a.exe`
  (ARM64); we pick by `runtime.GOARCH`, match members case-insensitively, write via `.part` +
  rename so a crash can't leave a truncated "installed" exe.
- `OpenAutologon(ctx)` pre-accepts the Sysinternals EULA in HKCU
  (`Software\Sysinternals\Autologon\EulaAccepted=1`, suppresses the first-run nag) and launches the
  tool **elevated** via the existing `runElevated` (Autologon needs admin to write the LSA secret +
  `HKLM\...\Winlogon`). One UAC prompt; we stay unelevated.
- 🔴 **We never pass credentials.** Launched with no args → Autologon shows its GUI; the user types
  the password and clicks Enable. Same "we never see the password" guarantee as netplwiz. Autologon
  stores it as an **encrypted LSA secret** (Microsoft docs note an admin can still decrypt it —
  inherent to autologon, true of netplwiz too).
- **No SHA pin.** Sysinternals ships no stable checksum endpoint and updates the tool periodically;
  we rely on HTTPS transport integrity + the Microsoft Authenticode signature UAC verifies at launch
  (mirrors shellmode's "install even without checksums" fallback).

Exposed as `App.OpenSysinternalsAutologon()`. The Settings "Авто-вход" card now has two buttons —
"🔓 Настроить через netplwiz" and "🔑 Настроить через Autologon" (with a download spinner state) —
and copy clarifying that the user types the password themselves either way.

### 2. BT add-device: `DevicePairingWizard.exe`

`OpenWindowsBluetoothSettings` now launches **`DevicePairingWizard.exe`** — the classic standalone
"Add a device" wizard that scans for and pairs Bluetooth/wireless devices. It's a plain Win32 exe in
System32, so (like `mmsys.cpl` for sound) it works in shell mode **without Explorer / the immersive
Settings app**. Fallback chain unchanged: `control.exe bthprops.cpl`.

`fsquirt.exe` is dropped — it was never the right tool.

## Consequences

- Two passwordless-logon paths; the user picks whichever the OS build cooperates with.
- The BT picker's "pair new" button now actually pairs. Connect/disconnect of paired devices is
  still the in-app `BluetoothPicker` (decision 0026) — this only fixes the pairing hand-off.
- `%LOCALAPPDATA%\GameSaver\bin\` may now hold `Autologon.exe` alongside `gamesaver-watchdog.exe`.

## Risks / known gaps

- `DevicePairingWizard.exe` is present on Win10/11; if a future Windows drops it, the `bthprops.cpl`
  fallback still opens *a* Bluetooth surface (may itself redirect to Settings on Win11).
- Autologon download depends on download.sysinternals.com being reachable; offline → clear error
  toast, netplwiz path still available.
