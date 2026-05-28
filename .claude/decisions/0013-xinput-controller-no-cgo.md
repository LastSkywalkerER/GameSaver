# 0013 — Xbox controller via XInput (syscall, no cgo); pause during games

Status: accepted
Date: 2026-05-27 (v0.4.0; hardened through v0.7.6)

## Context
Console-style UX needs a gamepad. Want it without an external dependency.

## Decision
- Poll `xinput1_4.dll` via `syscall` at 50 Hz; emit `controller:state/button/nav` events with auto-repeat for nav. **Poll all 4 user slots**, latch the first connected.
- Expose `IsControllerConnected()` so a late-mounting UI can pull state (events aren't replayed).
- 🔴 **While a game is running, stop calling `XInputGetState` entirely** (not just stop emitting) — see attempts/pause-emit-but-keep-polling.md.

## Consequences
- No cgo, no third-party controller lib.
- The pause is critical: two processes polling a wireless pad contend at the driver level and the game drops inputs.
- The controller backing-off cleanly is what makes the launch→play→exit cycle work (rules/shell-mode.md).
