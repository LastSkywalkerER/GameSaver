# 0019 — Sleep via direct SetSuspendState (wake events disabled)

Status: accepted
Date: 2026-05-28 (v0.7.7)

## Context
The power-menu Sleep made the PC wake ~2 s after sleeping.

## Decision
Call `SetSuspendState(FALSE, FALSE, TRUE)` **directly** (bWakeUpEventsDisabled=TRUE), after enabling
`SE_SHUTDOWN_NAME`. The call blocks until resume, so we pause the controller around it. Fallback to
rundll32 only if the direct call fails.

## Why not rundll32
🔴 `rundll32 powrprof.dll,SetSuspendState` **ignores its arguments** and sleeps with wake events
ENABLED → a wake-armed device (wireless pad dongle, mouse, NIC) fires during the transition and bounces
the PC awake.

## Consequences
- Spurious immediate wake is gone; the hardware power button still wakes it.
- Wake-via-keyboard/mouse is disabled for sleeps WE trigger (per-suspend only) — acceptable for the console use case.
- Also fixed alongside: the picker reopening on the sleep-induced `display:changed` (decision 0015 suppression window).
