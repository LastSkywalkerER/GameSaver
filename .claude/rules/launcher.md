# Rule — Game launching

`internal/launcher` decides how to start a game from a `domain.Installation` that may carry both an
`ExePath` and a `LaunchURI` (deep link like `steam://`, `goggalaxy://`, `com.epicgames.launcher://`).

## Priority

1. **Steam** → prefer the `steam://` deep link **if its protocol is registered** (overlay, cloud saves, DRM; steam:// is registered whenever Steam is installed).
2. **Everyone else** (GOG, Epic, EA, Ubisoft, standalone, pirate) → prefer the **bare exe** when it's valid. This is exactly what a working desktop shortcut does.
3. Fall back to the deep link **only if** there's no usable exe **and** the protocol is registered.
4. Otherwise error with a clear message naming the missing protocol.

## 🔴 A deep link is only usable if its protocol is registered

`cmd /c start "" <uri>` returns **success** even for an unregistered scheme — the shell takes the
hand-off and then pops its own "can't open this link" dialog. So you cannot detect failure from
`openURI`'s error. **Check `HKCR\<scheme>` for the `URL Protocol` value first** (`protocolRegistered`).

**Incident:** Cyberpunk (GOG) shipped a `goggalaxy://` LaunchURI; GOG Galaxy wasn't installed; we
preferred the URI and `start` "succeeded" → user got a "goggalaxy" link error and the exe fallback
never ran. Fixed in v0.7.8.

## Notes

- `execProcess` runs the exe with `cmd.Dir = filepath.Dir(exe)` (working dir matters — many games load assets relative to cwd; the shortcut sets "Рабочая папка" too).
- An installation with `ExePath == "" || strings.HasSuffix(..., "_no_exe_")` has no usable exe.
- The playtime tracker matches running processes by exe basename, so launching via exe also makes session detection reliable.
