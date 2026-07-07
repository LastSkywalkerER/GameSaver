# GameSaver — Technical Audit (2026-07-07)

> Scope: (1) audio output-device switching, (2) Bluetooth discovery/connect, (3) backup & restore,
> (4) EA App library import, (5) logging & data-location robustness (surfaced during diagnosis).
> Reviewed against tag **v0.11.0** (`9ca28cd`) — the latest release *and* current `HEAD`. Note the
> in-repo docs are stale: `CLAUDE.md` says "current release v0.10.8". Everything below is verified
> against the v0.11.0 source. **No code was changed — findings and fix options only.**
>
> Severity: 🔴 data-loss risk · 🟠 feature is broken / lies to the user · 🟡 fragile / UX.
> Each finding: **How it works now** (with code) → **What's wrong** → **Fix options**.
>
> Environment confirmed during diagnosis: the **kiosk (shell) mode runs under the `Администратор`
> account** (autologin), desktop mode under `LastSkywalker`. This matters for §5.

---

## 1. Audio — output device switching in AudioPicker

**Reported symptom:** in AudioPicker the BT headphones (JBL Live) / speaker (Soundcore) are already
connected and show up, audio plays through them, but re-assigning the default from the app does not
work. From Windows the same devices switch with a single click.

### 1.1 Backend switch — `internal/audio/audio_windows.go`

**How it works now.** Enumeration uses the documented MMDevice API; the *switch* uses the undocumented
`IPolicyConfig` COM interface (`SetDefaultEndpoint`). The setter loops over two roles and **returns on
the first failing role** (`SetDefault`, lines 211-237):

```go
for _, role := range []int{eConsole, eMultimedia} {
    if hr := callVTable(policy, vtSet,
        uintptr(unsafe.Pointer(idPtr)), uintptr(role)); int32(hr) < 0 {
        return fmt.Errorf("SetDefaultEndpoint (role %d) hr=0x%x", role, hr)
    }
}
```

COM is (re)initialised on **every** call, on whichever goroutine/OS-thread Wails happens to run it on
(`coInit`, lines 323-332):

```go
func coInit() error {
    runtime.LockOSThread()
    r, _, _ := procCoInitializeEx.Call(0, coinitMultiThreaded)   // COINIT_MULTITHREADED
    if int32(r) < 0 { runtime.UnlockOSThread(); return fmt.Errorf("CoInitializeEx hr=0x%x", r) }
    return nil
}
```

🟠 **F1.1 — the role loop reports failure after a partial success.** `eConsole` is applied first; if it
succeeds the default has **already audibly switched**. If the second call (`eMultimedia`) then returns a
transient `HRESULT` — common on an endpoint that just became active, e.g. a BT sink that connected
moments ago — `SetDefault` returns an error even though the switch happened. **Tell-tale in the toast
text: `role 1`.**

🟡 **F1.4 — per-call `CoInitializeEx` on an arbitrary thread.** If that thread was already initialised as
STA by other native code, `CoInitializeEx(COINIT_MULTITHREADED)` returns `RPC_E_CHANGED_MODE
(0x80010106)` and the first press fails; the next press lands on a clean thread and works. (No in-process
STA contaminator was found, so this is the weakest hypothesis — but the pattern is inherently racy.)

### 1.2 Frontend — `frontend/src/components/shell/AudioPicker.tsx`

**How it works now** (`applyDevice`, lines 81-93):

```ts
async function applyDevice(dev: AudioDevice | undefined) {
  if (applying || !dev) return;
  if (dev.isDefault) { playBack(); onDone(); return; }          // early-out
  playSelect(); setApplying(true);
  try {
    await (api as any).SetDefaultAudioDevice(dev.id);
    api.Toast("success", `${dev.dataFlow === "render" ? "Вывод" : "Ввод"}: ${dev.name}`);
    await refresh();
  } catch (e) {
    api.Toast("error", "Сменить устройство: " + String(e));     // <-- no refresh() here
  } finally { setApplying(false); }
}
```

🟠 **F1.2 — the catch path never re-reads device state.** On error there is no `refresh()`, so the "по
умолчанию" (default) badge keeps showing the old device even when F1.1 already switched it — reinforcing
"it errored, so it didn't switch" when it actually did.

🟡 **F1.3 — early-out on `isDefault` is a silent no-op.** If Windows auto-promoted the headphones to
default on connect, clicking them hits `if (dev.isDefault) { playBack(); onDone(); }` — the modal just
closes with no action, which reads as "switching doesn't work".

### 1.3 Fix options (audio)

1. **Make `SetDefault` tolerant of partial role success** (backend). Attempt both roles, succeed if ≥1
   applied, `slog.Warn` the other, optionally retry the failed role once after ~500 ms:
   ```go
   var applied int; var lastHR uintptr
   for _, role := range []int{eConsole, eMultimedia} {
       hr := callVTable(policy, vtSet, uintptr(unsafe.Pointer(idPtr)), uintptr(role))
       if int32(hr) < 0 { lastHR = hr; slog.Warn("SetDefaultEndpoint role failed", "role", role, "hr", hr); continue }
       applied++
   }
   if applied == 0 { return fmt.Errorf("SetDefaultEndpoint failed hr=0x%x", lastHR) }
   return nil
   ```
   This matches what the Windows "Sound" dialog effectively delivers.
2. **Frontend verify-after-error** (`AudioPicker.tsx`): call `refresh()` in the catch, and if `dev.id`
   is now the default, replace the error toast with success.
3. **Soften F1.3**: instead of a silent close on `isDefault`, show "already the default output".
4. **Dedicated COM worker thread** (removes F1.4 entirely): one `LockOSThread`ed goroutine with a single
   `CoInitializeEx`, fed by a channel; every audio call runs on it. Also removes per-call init cost.
5. **Log resolves the ambiguity**: the toast/`slog` line `SetDefaultEndpoint (role N) hr=0x…` tells F1.1
   (role/hr) apart from F1.4 (`CoInitializeEx hr=0x80010106`). See §5 for why the kiosk log is empty.

---

## 2. Bluetooth — discovery & connect

**Reported symptom:** on connect, `BluetoothSetServiceState 0x110B` surfaces "error 0 — the operation
completed successfully"; sometimes "device unavailable" with no attempt/spinner. From Windows the same
headphones connect in one click.

### 2.1 Connect — `internal/bluetooth/bluetooth_windows.go`

**How it works now.** The A button → `app.go:1276 ConnectBluetoothDevice` → `ConnectEx`
(lines 408-464). "Connect" enables six audio-profile services on the paired device via
`BluetoothSetServiceState`:

```go
for i := range audioServiceGUIDs {                 // 0x110B A2DP-sink first, then 110A/110E/1112/111E/1108
    g := audioServiceGUIDs[i]
    r, _, _ := procBluetoothSetServiceState.Call(radio,
        uintptr(unsafe.Pointer(target)), uintptr(unsafe.Pointer(&g)), btServiceEnable)
    switch {
    case r == errSuccess:                 anyOK = true; allNoProfile = false
    case r == errServiceDoesNotExist || r == errNotFound: // clean skip
    case r == errDeviceNotConnected:      // 1167
        return ConnectResult{OK: false, Status: "unreachable",
            Detail: "device not in range or powered off — turn it on / put it in pairing mode"}, nil
    default:                              allNoProfile = false
    }
}
if anyOK { return ConnectResult{OK: true, Status: "connected", Detail: "audio enabled"}, nil }
```

🟠 **F2.1 (root cause of "can't switch") — "Connect" is a *service-enable*, not a connection, and it
false-positives.** `BluetoothSetServiceState(..., enable)` installs/configures the profile driver. For a
paired device whose services are **already enabled** (the normal steady state), it returns
`ERROR_SUCCESS` immediately **without touching the radio**. So `ConnectEx` reports `{ok, "connected"}`
and the UI toasts "Подключено" — **even when the headset is off or the audio is actually routed
elsewhere**. No ACL/A2DP link is initiated. The classic Win32 BT API has **no public "connect A2DP now"
call**; the flip only causes a real connection on a *fresh* enable (right after pairing, or after our own
Disconnect). This is exactly "already connected, shows connected, but can't switch".

🟠 **F2.2 — instant "unreachable" with no retry/loader.** On `errDeviceNotConnected (1167)` `ConnectEx`
returns immediately, without polling `fConnected` (the connection is asynchronous and needs a moment) and
without a spinner. Matches "doesn't even try, no loaders".

🟠 **F2.3 — "error 0 / operation completed successfully" is a latent formatting bug.** The only remaining
raw-errno format is `setServicesLocked` (used by `Disconnect`/`Pair`), line 528:

```go
firstErr = fmt.Errorf("SetServiceState %04X failed (code %d): %v", g.Data1, r, syscall.Errno(r))
```

In v0.11.0 `r == 0` is caught earlier (`continue`), so this *should not* emit "code 0" — which
contradicts your observation. Two explanations: (a) the exact string you recall is from a pre-v0.10.6
build; (b) it comes from another path (Discover/scan-error). The **log settles this** (§5). Regardless,
formatting a raw `syscall.Errno(r)` is fragile and reproduces the v0.10.6 class of bug — it should be an
explicit code→message map.

🟠 **F2.4 — classic BR/EDR only; BLE devices are invisible** (acknowledged at lines 22-25). A chunk of
modern peripherals and controllers pair over BLE and can never appear in the in-app list.

🟡 **F2.5 — discovery is a single ~6.4 s synchronous inquiry** (`cTimeoutMultiplier=5`), no repetition,
results delivered in one burst at the end. Windows scans continuously → "finds almost nothing".

🟡 **F2.6** — first inquiry results often have no name → "(unnamed)"; no radio-state check (BT off →
empty scan, no "turn Bluetooth on" hint); `ConnectBluetoothDevice` is a blocking Wails promise (up to
~6×30 s) with no timeout.

**Why Windows connects in one click:** the Windows shell uses WinRT
(`Windows.Devices.Bluetooth` / `DeviceInformationPairing`) plus a real KS-property reconnect on the audio
endpoint — not a classic service-flip. The classic API simply cannot do what the Windows dialog does.

### 2.2 Fix options (Bluetooth) — WinRT is on the table

You confirmed you are **willing to pull in WinRT**. That unlocks the two best options; both are doable
**without cgo** (WinRT is COM-ABI; use `github.com/saltosystems/winrt-go`, or hand-roll
`RoActivateInstance`/`IInspectable`/`HSTRING` the same way the current code hand-rolls `IPolicyConfig`).

1. **Real connect/disconnect via `IKsControl` + `KSPROPSETID_BtAudio`
   (`KSPROPERTY_ONESHOT_RECONNECT` / `ONESHOT_DISCONNECT`)** — the mechanism shipping tools (ToothTray et
   al.) use. The entire hand-rolled COM/vtable plumbing and MMDevice enumeration **already exists** in
   `internal/audio/audio_windows.go`; correlate endpoint↔MAC via the endpoint device path (it contains
   the BT address). Fast, actually connects, no driver churn. Fixes F2.1/F2.2 for audio devices.
   Trade-off: semi-documented KS property set (stable in practice), audio devices only.
2. **WinRT `DeviceWatcher` + `DeviceInformationPairing` + `Bluetooth(LE)Device`** — the same stack the OS
   uses. Fixes F2.4 (BLE), F2.5 (continuous scan with incremental results), F2.6 (live names, radio
   state), and gives robust pairing (`DeviceInformationCustomPairing` with the ConfirmOnly/DisplayPin
   ceremonies) in one move. This is the strategically correct long-term backend; the classic-Win32 file
   becomes a fallback. Trade-off: largest effort (WinRT async is `IAsyncOperation`/completion handlers +
   `HSTRING` marshalling). Recommended path given your WinRT go-ahead.
3. **Cheap interim (no WinRT), still worth doing first:**
   - Don't toast "Подключено" from a service-flip result alone — after enabling, **poll `fConnected` for
     a few seconds** and report what actually happened (fixes F2.1/F2.2 truthfulness).
   - Raise `cTimeoutMultiplier` to ~8 and **loop inquiries while the picker is open**, emitting per round
     (F2.5).
   - Replace `syscall.Errno(r)` with an explicit code→message map (F2.3).
   - Check `BluetoothFindFirstRadio` up front and surface a distinct "radio off" state (F2.6).

**Doc drift:** the v0.11.0 BT rewrite has no `decisions/` file; `windows-syscalls.md` and decision 0028
describe the pre-release behaviour (the "via Windows" button is now `ms-settings:bluetooth`,
`app.go:1290-1312`). Per the repo's own self-maintenance protocol this should be recorded.

---

## 3. Backup & restore — `internal/backup/engine.go`

Baseline contract holds, but there are **two P1 data-loss paths** and **zero tests** in this package.

**✅ Verified correct:** zip write is crash-atomic (`.tmp` → `os.Rename`, lines 385/429; every error path
removes the tmp); retention prunes only *after* a successful insert (`applyRetention` at line 265, called
last in `snapshotLocation`); the UI always restores with `overwrite=true` (`GameDrawer.tsx:188`, sole
call site); zip-slip is rejected (lines 449-455); content-hash dedup skips unchanged saves (lines
199-202); reconcile re-imports orphan zips and prunes dead rows; restore is extract-over (never deletes
target files).

### 3.1 🔴 F3.1 (P1) — single-file save locations: no pre-restore backup **and** broken restore

**How it works now.** `Restore` gates the safety snapshot on the path being a *directory* (lines
314-318), then always calls `unzipInto`:

```go
// Safety snapshot first
if util.IsDir(loc.Path) {
    _, _ = e.snapshotLocation(g, loc, domain.TriggerPreRest)
}
return unzipInto(sn.ArchivePath, loc.Path, overwrite)
```

`unzipInto` then unconditionally `MkdirAll`s `dest` (line 445) — where `dest` is the *file* path for a
single-file location:

```go
if err := os.MkdirAll(dest, 0o755); err != nil { return err }   // dest == the save file's own path
...
target := filepath.Join(dest, filepath.FromSlash(f.Name))       // -> save.dat/save.dat
```

**What's wrong.** `scanFiles`/`writeZip` fully support single-file locations (lines 362-372, 390-395),
but on restore: (a) the `IsDir` gate skips the pre-restore snapshot, so the live single file is
overwritten **with no safety copy** — a red-line violation; (b) `MkdirAll(dest)` either fails (a file of
that name exists) or creates a **directory named like the file** and extracts to `save.dat\save.dat` —
the game never sees it. Single-file restore has effectively never worked.

**Fix.** Remove the `IsDir` gate (always snapshot first — see F3.2). In `unzipInto`, detect a file-typed
destination and extract to `filepath.Dir(dest)` / write the single entry directly:
```go
if fi, err := os.Stat(dest); err == nil && !fi.IsDir() {
    // single-file location: extract the one entry over the file, atomically (temp + rename)
}
```

### 3.2 🔴 F3.2 (P1) — pre-restore snapshot failure is silently swallowed

**How it works now** (line 316): `_, _ = e.snapshotLocation(g, loc, domain.TriggerPreRest)`.

**What's wrong.** Disk-full, permission error, or a fully-locked save dir → the safety snapshot silently
fails and restore proceeds to overwrite the live save anyway. This is the exact scenario the red line
("Restore must auto-backup the current save first") exists for.

**Fix.** Abort the restore when the pre-restore snapshot errors (or require an explicit `force`):
```go
if _, err := e.snapshotLocation(g, loc, domain.TriggerPreRest); err != nil {
    return fmt.Errorf("pre-restore backup failed, aborting restore: %w", err)
}
```

### 3.3 🟠 F3.3 (P2) — locked/changed files silently vanish; hash then hides the gap

**How it works now.** `scanFiles` returns `nil` (skips) on any open error (`os.Open` fails → `return nil`
in the WalkDir callback), and `writeZip` `continue`s on open failure (lines 397-399). Scan and zip are
two separate passes (TOCTOU).

**What's wrong.** A backup taken while a game holds files open is silently incomplete; the manifest may
list files the zip doesn't contain; and the dedup content-hash marks the truncated state as "latest"
(line 199), **suppressing the next correct backup**. Manual `BackupGame` (`app.go:518`) has no
running-game guard, unlike the watcher. Zero user-visible warning.

**Fix.** Count skip/open failures in `scanFiles`/`writeZip` and propagate; fail (or warn-to-UI) when
`skipped > 0`; add the watcher's `isAnyInstallRunning` check to manual `BackupGame`; don't record the
dedup hash for an incomplete capture.

### 3.4 🟠 F3.4 (P2) — restore is not atomic; no running-game guard

`unzipInto` extracts file-by-file over the live dir; a crash or a locked-file `os.Create` mid-way leaves
a half-old/half-new save (violates "either old or new state"). Nothing blocks restoring while the game
runs. **Fix:** extract to a temp sibling dir then swap, or pre-verify every entry is creatable before
writing the first; block restore while the target game's process is running.

### 3.5 🟠 F3.5 (P2) — retention can prune the snapshot being restored

`Restore` → pre-restore `snapshotLocation` → `applyRetention` (line 265) may delete the oldest
snapshots, including the one whose `ArchivePath` was fetched at line 302; the following `unzipInto` then
fails. No data loss (current state was just backed up), but the chosen restore point is destroyed by the
restore attempt. **Fix:** exempt `preRestore`/`preMigrate` triggers (or the in-flight snapshot ID) from
retention.

### 3.6 🟡 F3.6 (P3) — misc

- **Junctions/symlinks:** `filepath.WalkDir` does not follow symlinks; a save root that is an NTFS
  junction is seen as a non-dir, `os.Open` fails, `scanFiles` returns 0 files → snapshot silently skipped
  (`snapshotLocation` lines 195-197). Given migrations are "copy + NTFS junction", test this on Windows.
- **Orphaned zip on manifest-write failure:** manifest write is only `slog.Warn` (line 238); reconcile
  imports only zips *with* a manifest, so such a zip can never be re-imported after DB loss. Fail the
  backup (delete zip) or retry.
- **`AppVersion` hardcoded `"0.1.0"`** in the manifest (line 228) at release v0.11.0 — forensics value
  lost; inject the real version.
- `DeleteSnapshot` ignores `os.Remove` errors and deletes the row anyway (lines 294-296) → a locked zip
  gets re-imported by the next reconcile; pruning never converges.

### 3.7 Tests — none

`internal/backup` (the "a bug = lost save data" package) has **zero** `*_test.go`. All the pure helpers
are OS-portable and trivially testable. You confirmed tests are wanted — highest-value/lowest-cost
hardening in the repo:
- `unzipInto`: zip-slip rejection, single-file dest (F3.1), overwrite=false semantics, Chtimes.
- `scanFiles`: stable `contentHash` (order independence), single-file root, skip-on-error counting.
- `applyRetention` + `OldestSnapshots`: ordering and the preRestore-exemption (F3.5).
- `Reconcile`: orphan import, slug-collision fallback, dead-row prune.

---

## 4. EA App library import — full owned library

**Goal:** the full owned library, not just installed games.

### 4.1 Installed detection today — `internal/scan/launchers/ea.go`

Registry Uninstall-key scraping (HKLM/WOW6432Node/HKCU), kept if `Publisher` contains "electronic
arts"/"ea games" or `UninstallString` contains "ea desktop". Gaps: misses titles whose publisher is a
studio label (Respawn/BioWare/Maxis); drops any game without a confidently-scored exe; sets no
`LaunchURI`/`SourceAppID`.

### 4.2 Online provider exists but is disabled — `internal/stores/ea.go`

**How it works now.** The dormant provider targets the legacy Origin endpoints and `LoginURL()` returns
empty with the comment "there is NO browser-paste path to the EA cloud library." The blocker is a wrong
client id (line 28):

```go
const (
    eaClientID = "EADOTCOM-WEB-SERVER"                          // <-- rejected by the entitlements gateway
    eaEntURL   = "https://api1.origin.com/ecommerce2/entitlements/"   // <-- wrong path (see below)
)
```

🟠 **F4.1 — wrong `client_id`; the comment "no path" is outdated.** `EADOTCOM-WEB-SERVER` is a web-server
client the entitlements gateway rejects ("auth token not allowed for unknown client"). Three live
projects (Lutris `ea_app.py`, FriendsOfGalaxy `galaxy-integration-origin`, and the EA-App fork
`BellezaEmporium/galaxy-integration-ead`) all use **`ORIGIN_JS_SDK`** — a public first-party JS-SDK
client the same endpoints trust, and it **is browser-capturable** (unlike the desktop
`JUNOPCCLIENT`/`ORIGIN_PC` clients, which use a `qrc://` redirect you can't capture — the real reason for
the old comment; it does **not** apply to `ORIGIN_JS_SDK`).

**Working flow (high confidence — corroborated across all three tools):**
1. Open a browser/WebView2 to the EA login; the user logs in (2FA/captcha on them).
2. Capture the **`remid`** (long-lived) and **`sid`** cookies on `accounts.ea.com`.
3. Silent GET
   `accounts.ea.com/connect/auth?client_id=ORIGIN_JS_SDK&response_type=token&redirect_uri=nucleus:rest&prompt=none`
   with those cookies → returns **JSON** `{access_token,…}` directly (because `redirect_uri=nucleus:rest`
   — this is what sidesteps the `qrc://` problem).
4. Token refresh = re-issue the same silent request using the persistent `remid`.
5. Library — either:
   - Legacy REST `ecommerce2/consolidatedentitlements/{pid}?machine_hash=1` **+ header
     `Accept: application/vnd.origin.v3+json; x-cache/force-write`** (current code uses the different
     `entitlements/{pid}` path, `ea.go:32`), or
   - **Juno GraphQL (current EA App)** `https://service-aggregation-layer.juno.ea.com/graphql`, query
     `ownedGameProducts(storefronts:[EA], type:[DIGITAL_FULL_GAME,PACKAGED_FULL_GAME], platforms:[PC])`
     — returns ownership **and** names in one call (replacing the per-offer catalog lookups in
     `offerNames`, which is where the current code also failed).
6. All three tools set `Authorization` + `AuthToken` + **`X-AuthToken`**; the current code sets only the
   first two (`ea.go:157-159`).

**Concrete edits to revive `internal/stores/ea.go`:**
- `eaClientID` → `ORIGIN_JS_SDK`; use `response_type=token`, `redirect_uri=nucleus:rest`, `prompt=none`.
- Add cookie capture (`remid`/`sid`); store `remid` in the DPAPI vault as a secret (per `secrets.md`).
- Replace `FetchOwned` with `consolidatedentitlements` + the `Accept` header, or (preferred) the Juno
  GraphQL query.
- Add the `X-AuthToken` header.
- `LoginURL()` (lines 40-48): return a working URL/hint instead of the "no path" message.
- Caveat: `accounts.ea.com` needs legacy TLS renegotiation — Go's `http.Client` may need a
  `tls.Config`/transport tweak (Lutris uses a custom adapter for exactly this).

**Confidence caveat:** WebFetch summarised the three source files rather than returning raw text — verify
exact variable names/paths by reading them directly before implementing. The
`ORIGIN_JS_SDK` + `nucleus:rest` + `prompt=none` flow and the Juno endpoint are corroborated across all
three, so those are high-confidence.

### 4.3 Cheap installed-detection upgrades (verified against erri120/GameFinder wiki)
- `HKLM\SOFTWARE\WOW6432Node\Origin Games` — one subkey per installed game; subkey name = contentID,
  value = install path. Survived the Origin→EA App migration.
- `__Installer\installerdata.xml` inside each game dir — `<contentID>` + the registry key the game writes.
- Launch fallback `origin2://game/launch/?offerIds=<contentIDs>` (not `link2ea://`, which doesn't open
  the app — already noted at `app.go:1331`).
- (High maintenance risk) decrypt `%ProgramData%\EA Desktop\<hash>\IS` (AES-256-CBC, hardware-derived
  key) — reference impl in GameFinder; the key breaks on CPU/GPU/board/C:-drive changes.

**Sources:** `github.com/erri120/GameFinder/wiki/EA-Desktop`; `lutris/lutris` (`ea_app.py`, issues
#4996/#5591); `FriendsOfGalaxy/galaxy-integration-origin`; `BellezaEmporium/galaxy-integration-ead`.

---

## 5. Logging & data-location robustness (surfaced during diagnosis)

**How it works now** — `internal/logging/logging.go:16-27`:

```go
f, err := os.OpenFile(filepath.Join(cfg.LogsDir(), "gamesaver.log"),
    os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
...
h := slog.NewJSONHandler(w, &slog.HandlerOptions{Level: slog.LevelInfo})
slog.SetDefault(slog.New(h))
```

`cfg.LogsDir()` = `filepath.Join(UserDataDir, "logs")`, and `UserDataDir` is derived purely from the OS
(`internal/config/config.go:59-70`):

```go
func UserDataDir() (string, error) {
    base, err := os.UserCacheDir()          // Windows: %LOCALAPPDATA% (account- and MSIX-dependent)
    ...
    return filepath.Join(base, appName), nil // appName = "GameSaver"
}
```

`main.go:35-42` calls `config.Load()` then `logging.Setup(cfg)`, but swallows a Setup error:
```go
if err := logging.Setup(cfg); err != nil { slog.Error("setup logging", "err", err) }  // continues anyway
```

**Evidence gathered.** A whole-disk search found `gamesaver.log` in three places:

| Path | Size | State |
|---|---|---|
| `C:\Users\`**`Администратор`**`\AppData\Local\GameSaver\logs\gamesaver.log` | 210047 | **all `0x00`** (the file you uploaded) |
| `…\Администратор\…\Packages\Claude_pzs8sxrjxfjjc\LocalCache\Local\GameSaver\logs\gamesaver.log` | 13594 | has data |
| `…\LastSkywalker\…\Packages\Claude_pzs8sxrjxfjjc\LocalCache\Local\GameSaver\logs\gamesaver.log` | 56319 | has data |

🟠 **F5.1 — logs are not durable (no `fsync`).** The writer never calls `f.Sync()`. The uploaded kiosk
log was 210 KB of pure `0x00` — classic NTFS zero-fill: the file *size* was recorded but the data blocks
were never flushed before a hard power event (sleep/power-cut). On a kiosk that sleeps/powers off, the
log is lost exactly when a hang/crash needs diagnosing. **Fix:** `f.Sync()` after each write (or a
periodic flush timer); optionally mirror critical errors to the Windows Event Log; add rotation.

🟠 **F5.2 — the live kiosk isn't writing where expected.** The uploaded file was pulled from a *running*
kiosk yet is zero to the last byte; with `O_APPEND`, the current session's lines would be at the tail, so
the running process wrote nothing to it. Combined with the search result, the kiosk (autologin
`Администратор`) either logged to the MSIX-redirected path or `logging.Setup` failed and `main.go`
swallowed it. **Fix:** don't swallow the `Setup` error in kiosk (surface it on-screen — a kiosk has no
console); log the resolved `UserDataDir` at startup.

🔴 **F5.3 — data is fragmented across accounts and MSIX containers (library/backup desync risk).** Because
the data dir comes from `os.UserCacheDir()` / `os.UserConfigDir()`, it depends on (a) the process account
and (b) MSIX redirection. On this machine data exists under the `Администратор` profile (kiosk), the
`LastSkywalker` profile (desktop), **and** both again inside `Packages\Claude_pzs8sxrjxfjjc\LocalCache`.
The same logic governs **`gamesaver.db`, `settings.json`, secrets, and (by default path) not backups**
(`BackupRoot` defaults to the absolute `C:\GameSavesBackup`, so backups are the one shared artifact).
Consequence: desktop and kiosk can open **different databases** → different library/matches/snapshot
history; a backup made in one mode isn't visible in the other except via reconcile over
`C:\GameSavesBackup`. For an app whose kiosk and desktop are two modes of one install, this is a hidden
source of "missing" data.

**Fix options (F5.3):** anchor data to a fixed, account/MSIX-independent path — next to the exe (fits the
portable-zip distribution) or `%ProgramData%\GameSaver` with a shared ACL — so kiosk and desktop share
one DB/settings/log; at minimum, log the resolved `UserDataDir` on every start so a desync is visible.

---

## Appendix — capturing a usable log for §1/§2 diagnosis

The two non-zero logs above may already contain the audio/BT errors. Grep them:
```powershell
Select-String -Path 'C:\Users\Администратор\AppData\Local\Packages\Claude_pzs8sxrjxfjjc\LocalCache\Local\GameSaver\logs\gamesaver.log',
  'C:\Users\LastSkywalker\AppData\Local\Packages\Claude_pzs8sxrjxfjjc\LocalCache\Local\GameSaver\logs\gamesaver.log' `
  -Pattern 'SetDefaultEndpoint','CoInitialize','SetServiceState','bluetooth','audio','hr=','code' |
  Select-Object Filename, LineNumber, Line
```
Look for:
- audio: `SetDefaultEndpoint (role N) hr=0x…` (→ F1.1, with the role/hr) vs `CoInitializeEx hr=0x…` (→ F1.4);
- BT: `SetServiceState … code …`, `bluetooth pair`, `bluetooth:scan-error` (→ confirms/denies F2.3).

If the non-zero logs don't contain them, fix logging first (F5.1: `f.Sync()`), then reproduce in a
session that isn't hard-powered-off.

---

## Suggested order of work (by risk)

1. **Logging & data location (§5)** — without a durable, single-location log, §1/§2 are debugged blind;
   F5.3 also silently splits the user's library/backups.
2. **Backup P1s (F3.1, F3.2) + tests (§3.7)** — direct save-data-loss paths.
3. **Audio (F1.1/F1.2)** — small, high-value; likely fixes the reported switch failure outright.
4. **Bluetooth (§2.2)** — interim truthfulness fixes now; then `IKsControl` reconnect + **WinRT
   `DeviceWatcher`** for real connect/BLE/pairing (WinRT approved).
5. **EA full library (§4.2)** — revive the provider on `ORIGIN_JS_SDK` + Juno GraphQL.
