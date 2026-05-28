# 0018 — Passwordless Windows logon via netplwiz (never touch the password)

Status: accepted
Date: 2026-05-27 (v0.6.1)

## Context
For a console-like boot, the user may want Windows to log in without a password prompt. But handling the
user's account password ourselves (LSA / AutoAdminLogon plaintext) is a security liability.

## Decision
Don't touch the password. The Settings button:
1. On Win11 22H2+, unhides the netplwiz "Users must enter a user name and password..." checkbox by setting `HKLM\...\PasswordLess\Device\DevicePasswordLessBuildVersion = 0` via a UAC-elevated `reg.exe` (one prompt; our process stays unelevated).
2. Launches `netplwiz` so the user unchecks the box and types their password into the OS UI. Windows stores it encrypted in LSA secrets.

## Consequences
- We never see or store the password.
- Requires one UAC prompt for the registry tweak on newer Win11.
- The user finishes the flow in the native dialog.
