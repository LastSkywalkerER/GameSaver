# Dead end — pausing controller EMIT but continuing to poll XInput

## Tried (v0.7.5)
While a game runs, keep the XInput poll loop calling `XInputGetState` at 50 Hz but skip emitting
`controller:nav`/`controller:button` events. The reasoning: stop the hidden UI from reacting, but keep
connection state fresh.

## Why it failed
The game still didn't see the controller. Two processes hammering XInput on a wireless pad contend at
the driver level — the controller's report stream gets serialised and the foreground game drops button
presses. Keyboard worked (so it looked like a focus bug), which sent the first diagnosis down the wrong
path.

## What works instead (v0.7.6)
When paused, `continue` **before** any `XInputGetState` call — back off the device entirely so the game
has it exclusively. Reset the button/nav baseline on resume so a held button doesn't replay. See
decision 0013, rules/windows-syscalls.md (🔴), protocol/structured-reasoning.md.
