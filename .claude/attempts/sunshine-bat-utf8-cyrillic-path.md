# Dead end — generating a UTF-8 .bat with a Cyrillic path in it

## Tried (v0.8.2 → failed in the field)

The Sunshine apply step wrote a `.bat` (UTF-8) that copied the staged apps.json
(`%LOCALAPPDATA%\GameSaver\sunshine-apps.staged.json` =
`C:\Users\Администратор\AppData\Local\GameSaver\…`) into Program Files, then
restarted the service. Ran it elevated via ShellExecuteEx.

## Why it failed

`cmd.exe` reads **.bat files in the console OEM codepage**, not UTF-8. The
Cyrillic in the staged file's path (`Администратор`) was mis-decoded, so `copy`
got a garbled source path → "file not found" → `exit /b 1`. The user saw
"ОШИБКА: apply завершился с кодом 1" and none of the step lines, because the
copy died on line 1. Running GameSaver as admin made no difference — the bug
was the codepage, not privileges.

## What works instead (v0.8.4)

Don't write a .bat. Pass the whole command **inline via ShellExecuteEx
lpParameters**, which is UTF-16 (the native Windows command-line encoding) —
Cyrillic paths survive intact. Prepend `chcp 65001>nul` so net/copy output
lands in the tailed log as readable UTF-8. ASCII `[n/5]` step echoes keep the
progress lines clean.

Lesson: any time we hand a path with non-ASCII to `cmd`, prefer an **inline
UTF-16 command line** over a generated batch file. 8.3 short paths are not a
reliable workaround (8dot3 can be disabled per-volume).
