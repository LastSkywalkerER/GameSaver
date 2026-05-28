# Dead end — adding "safety" padding to the DEVMODEW Go struct

## Tried
When hand-writing `devModeW` (DEVMODEW) in Go for `ChangeDisplaySettingsExW`, added two extra
`_pad uint16` fields "to be safe" about alignment.

## Why it failed
DEVMODEW is exactly 220 bytes. The padding made the Go struct 224 bytes, shifting every field after the
union by 2–4 bytes:
- enumeration read the monitor's height into the width field → picker showed "2160 × 0";
- `ChangeDisplaySettingsExW` returned `DISP_CHANGE_BADMODE (-2)` → monitors wouldn't switch at all.

## What works instead
Match the MSDN layout field-for-field and let **Go's natural alignment** insert the same padding the C
compiler does (a `uint16` before a `uint32` auto-pads 2 bytes — you don't add it yourself). Verify with
`unsafe.Sizeof == 220`. See decision 0014, rules/windows-syscalls.md (🔴).
