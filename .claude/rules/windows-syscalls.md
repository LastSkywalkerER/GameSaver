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

## General

- Always merge stderr / check return codes; many Win32 calls return 0 on failure with `GetLastError` in the third `Call` return.
- Keep Win32 code in `*_windows.go` files so a non-Windows `go vet`/tooling pass doesn't choke.
