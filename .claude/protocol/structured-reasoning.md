# Protocol — Structured reasoning (for the loop-class bugs)

Most of GameSaver's nastiest bugs share one shape: **our own action emits an event that re-triggers our
own action.** When a report sounds like "X keeps happening / comes back / fights me", do not patch the
symptom — find the loop.

## The drill

1. **Name the user-visible symptom precisely.** "Picker reopens right after I choose a monitor." / "PC sleeps then wakes in 2 s." / "Game doesn't see the pad but keyboard works."
2. **List what WE did right before it.** Did we call `MakeSole` / `SetSuspendState` / `MinimizeSelf` / `LaunchGame`?
3. **List what events that action emits.** `MakeSole` → topology change → `display:changed`. `SetSuspendState` → displays power down → `display:changed`. Launch via Steam → focus stays with us.
4. **Ask: does an event from step 3 feed back into the action from step 2?** That's the loop.
5. **Break it with the smallest possible gate**, and write down WHY in a comment:
   - settling window (ignore self-inflicted `display:changed` for N s after a pick),
   - suppression window (ignore `display:changed` during sleep transition),
   - pause flag (stop polling XInput while a game runs),
   - debounce (coalesce a burst of `display:changed` into one evaluation).
6. **Re-check the OTHER direction.** Wall-clock windows must let the *legitimate* later event through (a real wake hours later should still show the picker).

## Worked examples (all real)

- **Picker re-opens after pick** → `MakeSole` disables monitors → `display:changed` → reopen. Fix: 30 s settling + silent re-assert. (v0.7.2)
- **Sleep wakes instantly** (two causes): (a) `rundll32 SetSuspendState` ignores args → wake events enabled → device wakes it; (b) the picker reopening on the power-down `display:changed` re-enabled monitors and aborted sleep. Fix: direct `SetSuspendState(...,TRUE)` + 20 s suppression window. (v0.7.3, v0.7.7)
- **Game doesn't see pad** → we minimized but kept polling XInput 50 Hz → driver contention. Fix: stop polling entirely while paused. (v0.7.6)
- **Game launches behind us** → game started by launcher (not foreground) can't beat our foreground lock. Fix: minimize self on launch, restore on exit. (v0.7.4)

## When the premise might be wrong

If the user says "still broken" after a fix you believed in, the premise (your model of the loop) is
probably wrong. Re-run the drill from step 1 with their new detail, rather than defending the previous
fix. The XInput "game doesn't see the pad" bug took two iterations (stop-emit → stop-poll) precisely
because step 3 was incomplete the first time.
