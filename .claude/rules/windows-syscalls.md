# Rule — Windows syscalls & Win32 ABI

GameSaver is Windows-only and talks to Win32 directly from Go via `syscall.NewLazyDLL` (XInput,
user32, powrprof) and `golang.org/x/sys/windows` (registry, tokens). These are sharp tools.

## 🔴 Struct layouts must match the C ABI byte-for-byte

When you pass a `*struct` to a Win32 function, Windows reads/writes it at fixed offsets. If your Go
struct's size or field offsets differ by even one byte, **every field after the mistake is wrong** and
the call may be silently rejected.

**Incident (the canonical one):** `devModeW` (DEVMODEW) is 220 bytes. Two "just in case" `_pad uint16`
fields made it 224, shifting `PelsWidth`/`PelsHeight` by 4 bytes:
- enumeration read monitor height as width → picker showed "2160 × 0"
- `ChangeDisplaySettingsExW` returned `DISP_CHANGE_BADMODE (-2)` → monitors wouldn't switch.

Rules:
- **Never add padding fields "to be safe".** Go's natural alignment already inserts the same padding the C compiler does (e.g. a `uint16` followed by a `uint32` auto-pads 2 bytes). Match the MSDN layout field-for-field and let Go align.
- **Verify `unsafe.Sizeof`** against the documented `sizeof` when in doubt (DEVMODEW = 220, DISPLAY_DEVICEW = 840, XINPUT_STATE = 16).
- Set the struct's `Size`/`cb` field where the API requires it (`dm.Size = uint16(unsafe.Sizeof(dm))`, `dev.cb = uint32(...)`).

## 🔴 Passing negative ints to syscalls

`SetWindowPos` etc. take 32-bit signed `int` args (e.g. a monitor at x = -1920). Across `syscall.Call`,
each arg is a `uintptr`. `uintptr(x)` on a negative value sets the high 64 bits wrong.

Use: `uintptr(uint32(int32(x)))` — packs the value into the low 32 bits so the C side reads the correct
signed `int`. See `winutil.i32`.

## XInput (`internal/controller`)

- Use `xinput1_4.dll` (ships with Win10+). `procGetState.Find()` once; if missing, no-op the feature.
- **Poll all 4 user slots**, latch onto the first connected one, re-scan only when it disconnects. Incident: polling only slot 0 missed a pad assigned to slot 1+.
- **While a game runs, stop polling entirely** (not just stop emitting). See `decisions/0013` and `attempts/pause-emit-but-keep-polling.md`. 🔴 red line.
- `XInputEnable` is a no-op in 1.4 — don't rely on it.

## Display (`internal/display`)

- Enumerate with `EnumDisplayDevicesW`; current mode via `EnumDisplaySettingsExW(ENUM_CURRENT_SETTINGS)`.
- "Disable a monitor" = DEVMODE with `DM_POSITION|DM_PELSWIDTH|DM_PELSHEIGHT`, all zero, flag `CDS_UPDATEREGISTRY|CDS_NORESET`, then commit with `ChangeDisplaySettingsExW(NULL,NULL,NULL,0,NULL)`.
- **You can't disable the current primary** without first promoting the target with `CDS_SET_PRIMARY` at (0,0). Restore does the reverse (primary first).
- Snapshot the prior layout to `%LOCALAPPDATA%\GameSaver\display-backup.json` before changing, so we can restore on shell exit.

## Power (`internal/power`)

- `LockWorkStation` (user32) — no privilege needed.
- 🔴 **Sleep via direct `SetSuspendState(FALSE, FALSE, TRUE)`** (bWakeUpEventsDisabled=TRUE) after enabling `SE_SHUTDOWN_NAME`. **Never** `rundll32 powrprof.dll,SetSuspendState` — it ignores args and wakes immediately. The call blocks until resume. See `decisions/0019`.

## Window (`internal/winutil`)

- Find our own window with `FindWindowW(NULL, "GameSaver")` (the Wails title).
- `SpanVirtualScreen` uses `GetSystemMetrics(SM_*VIRTUALSCREEN)`; `MoveToRect` snaps back to one monitor. Used by the multi-monitor picker.

## Bluetooth (`internal/bluetooth`)

- 🔴 **`BluetoothSetServiceState` reports its result in its DWORD RETURN VALUE, not `GetLastError`.** Read `r` (the 1st `proc.Call` return) and compare to `ERROR_SUCCESS (0)`. The 3rd `Call` return (GetLastError) is `Errno(0)` ("The operation completed successfully") on this no-SetLastError path. Incident (v0.10.6): we formatted the 3rd return, so every connect failure toasted the nonsensical `errno 0: The operation completed successfully`. Know which convention an API uses before formatting its error.
- 🔴 **`BluetoothSetServiceState` needs a real radio handle — never `NULL`.** Win10/11 reject `hRadio=NULL` with `ERROR_INVALID_PARAMETER (87)`, so no service flips and no connection is attempted. Get one from `BluetoothFindFirstRadio` (fallback to NULL only if there's genuinely no radio); release the radio `HANDLE` with `CloseHandle` and the find handle with `BluetoothFindRadioClose`. Incident: v0.10.6 BT picker connected nothing.
- `BLUETOOTH_DEVICE_INFO` is 560 B on x64; `BLUETOOTH_DEVICE_SEARCH_PARAMS` needs the explicit pad before `hRadio` (set every `dwSize`). On a service flip, `ERROR_SERVICE_DOES_NOT_EXIST (1060)` / `ERROR_NOT_FOUND (1168)` = the device doesn't expose that profile → clean skip, not a failure.
- 🔴 **"Add a device" = `DevicePairingWizard.exe`, NOT `fsquirt.exe`.** `fsquirt.exe` is the *Bluetooth File Transfer* wizard (send/receive files) — launching it for "pair new device" drops the user into a file-transfer flow. `DevicePairingWizard.exe` is the standalone pairing wizard and (like `mmsys.cpl`) works in shell mode without Explorer. Incident: v0.10.1–v0.10.7, fixed v0.10.8. See [decision 0028](../decisions/0028-sysinternals-autologon-and-bt-pairing-wizard.md).

## General

- Always merge stderr / check return codes. **Most** Win32 calls return 0 on failure with the real code in `GetLastError` (the third `Call` return) — but **some report the error in the return value itself** (e.g. `BluetoothSetServiceState` returns a Win32 DWORD; its `GetLastError` is meaningless). Check which convention the API uses. See the Bluetooth section.
- Keep Win32 code in `*_windows.go` files so a non-Windows `go vet`/tooling pass doesn't choke.
