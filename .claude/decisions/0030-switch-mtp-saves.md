# 0030 — Nintendo Switch saves over MTP (WPD), isolated from the Windows library

**Status:** accepted · 2026-09-06

## Context

A Switch running DBI's MTP responder exposes 8 stores; store "N: Saves" holds
`Installed games/<Title>/<Profile>/<files…>` (plus `Uninstalled games/`). We want
to browse it, back it up, and move saves between console and PC.

Measured on the reference console (serial `xtj10668389812`): 74 installed + 8
uninstalled titles, 104 profile entries, 419 files, 259 MB.

## Decisions

### 1. Transport is WPD COM, called through raw vtables

- **Not libmtp / go-mtpfs** — they need libusb, which on Windows means replacing
  the device driver with Zadig and breaking MTP in Explorer for the user.
- **Not `Shell.Application.CopyHere`** — works for reads but is asynchronous and
  reports no errors, so a failed transfer is indistinguishable from a slow one.
- go-ole is *not* promoted to a direct dependency: `x/sys/windows` already has
  `CoInitializeEx`/`CoTaskMemFree`, and the one missing function
  (`CoCreateInstance`) is a three-line `ole32.dll` binding.

**CLSIDs and IIDs were read out of the registry, not recalled.** This caught a
real error: `IPortableDeviceValues` is `{6848F6F2-3155-4F86-B6F5-263EEEAB3143}`,
not the value initially written down. A wrong IID fails silently as a
QueryInterface miss. `HKLM\SOFTWARE\Classes\Interface` maps names → IIDs; use it.

PROPERTYKEYs are *not* registry-discoverable, so they were verified empirically
against the device via `cmd/gs-switch` — the counts it reported matched an
independent Shell-COM enumeration exactly, and a file read back byte-identical
(same SHA-256) through both APIs.

### 2. The second tree level is a user profile, not a save-data type

`user`, `SkywalkerC`, `LyolikC` are console account nicknames; `BCAT`, `Device`,
`SD_Cache.*` are the actual save-data types. **26 of 74 titles carry saves for
two accounts and Minecraft for three**, so profile is part of the backup
identity everywhere. Backing a title up as one unit would merge two people's
saves into a single archive.

### 3. Console backups live in `<BackupRoot>\.switch\` and have no DB rows

`backup.Reconcile` already skips top-level directories beginning with `.`, so
the dot prefix is load-bearing — it keeps console snapshots out of the Windows
game library, out of `BackupAll`, and out of the fsnotify watcher (which cannot
work over MTP anyway).

Disk is the source of truth for them, consistent with `backup-restore.md`: the
DB is a cache. Verified after the fact — with a console backup present, startup
reconcile still reported `scanned: 4, createdGames: 0` and no phantom game
appeared on the dashboard.

`SnapshotManifest` gained optional `platform` / `switchTitle` / `switchProfile`
/ `switchSerial` fields. Empty `platform` means "windows" (`PlatformOrDefault`),
so every manifest written before this change still parses.

### 4. DBI reports no mtime, so dedup is content-hash only

No object on the device carries a modification or creation time. Dedup reuses
the Windows engine's scheme (sha256 over sorted `rel` + per-file hash), and zip
entries are stamped with the snapshot time rather than a 1979 zero value.

Consequence: unlike the Windows engine, the backup makes a **single pass over
the wire** — bytes stream from MTP into the zip while being hashed, and the
dedup check happens after. Re-reading to hash first would double transfer time
and double the window in which the user can unplug the cable.

### 5. Read sessions ask the driver for GENERIC_READ

`WPD_CLIENT_DESIRED_ACCESS` is set to `GENERIC_READ` for scanning and backup, so
those paths *physically cannot* modify the console — the guarantee is enforced
by the driver, not by our control flow. Only `TransferPCToSwitch` opens a
writable session.

### 6. PC → Switch re-plans inside the writable session

MTP object ids are only meaningful within the session that enumerated them, so
replaying a plan built in an earlier session could delete the wrong object. The
preview (`PreviewSwitchUpload`, read-only) is for the user to inspect; `Upload`
recomputes the plan and executes that one.

Files are deleted then re-created because MTP has no truncate — writing to an
existing name yields a second object with a duplicate name.

🔴 The pre-transfer backup of the target console profile is unconditional and
its failure aborts the transfer. There is deliberately no "skip backup" flag.

## Verified

Read paths end-to-end through the real UI against the attached console:
detection → scan (51 titles with saves) → multi-profile select → backup → zip
extracted byte-identical → platform badges on the Backups page → reconcile
isolation.

The write path was then verified too, on `Biomutant / SkywalkerC` (game not
installed, so the save is orphaned data) with the user's explicit permission:

1. **Identical restore** — restoring the backup onto its own profile left both
   files byte-identical. Necessary but *not sufficient*: a write that silently
   did nothing would pass this too.
2. **Mutation test** — flipped the last byte of `BIOMUTANT_Settings`, uploaded,
   and read back `780611d3…` instead of `afbe1875…` while the untouched
   `BIOMUTANT_AutoSave00` kept its hash. This is the one that proves DBI
   accepts `SendObject` into the Saves store *and* commits it.
3. **Rollback** — restoring the original archive returned both hashes to their
   starting values.

The mandatory pre-backup fired as designed: the restore in step 3 captured the
mutated state as a `preRestore` snapshot, and the identical restore in step 1
was correctly deduped away instead of writing a redundant archive.

## Shell mode

Shell mode gets its own `ShellSwitchView` overlay (`Overlay = … | "switch"`,
plus a 🎮 corner icon carrying a presence dot) at full feature parity with the
desktop page. It is a separate component, not a reuse of `SwitchPage`: that
page is built from `<select>`, checkboxes and `<Modal>`, none of which a
gamepad can drive — the same reasoning `ShellBackupsView` records for not
reusing the backups table.

The overlay is a step machine (`list → actions → pickLoc → confirm`, and
`list → restoreDest → restoreProfile|restoreLoc → confirm`) where each step is
a plain vertical list. **All controller handling lives in the one component**
and branches on the current step, per the "only one component-tree reacts to
the pad at a time" rule in `rules/frontend-ui.md`. Step objects carry the ids
they act on (and, for `confirm`, the closure to run), so no handler reads a
value out of a stale closure — the failure mode `frontend-ui.md` documents for
PowerMenu and AudioPicker.

🔴 Every console-writing path routes through the `confirm` step; there is no
way to reach `TransferPCToSwitch` or `RestoreSwitchBackupToConsole` from the
overlay without an explicit A-press on a confirmation screen.

The corner icon is always present even with no console attached, because
`CORNER_ICON_ORDER` is what maps d-pad focus to actions — omitting an entry
would renumber every icon after it.

## Destination filtering

Transfer and restore-to-PC pickers list only save folders of a PC game whose
name matches the console title. Offering every game let you push a Cyberpunk
save into Biomutant's console slot, which can only corrupt it.

Matching normalises (lowercase, drop ®™©, drop non-alphanumerics), strips
edition tokens, then compares for **exact equality**. Substring matching was
rejected: `hollowknight` is a prefix of `hollowknightsilksong`, and `doom` of
`doometernal`. Edition stripping is what makes "The Witcher 3 Wild Hunt —
Complete Edition" match a PC "The Witcher 3: Wild Hunt"; the token list is
ordered longest-first so `completeedition` is consumed before `edition`.

It cannot bridge localised names — the console lists "Ведьмак 3 Дикая Охота"
while the PC copy is "The Witcher 3", and that is a real case for this user. So
a no-match state offers an explicit, warned "pick a folder manually" override
rather than dead-ending. For the same reason the per-chip transfer button is
dimmed rather than hidden when there's no match.

## Restoring

`RestoreSwitchBackupToConsole` takes the destination profile as a parameter
rather than reusing the one in the manifest, because moving a save between two
console accounts is a main use case. The picker is populated from
`GetSwitchTitleEntries` (live, read-only) and offers only profiles that already
exist for that title — DBI maps them to real account save-data and we cannot
conjure a new one. `ExtractSwitchBackupToPC` is the console-free counterpart.
