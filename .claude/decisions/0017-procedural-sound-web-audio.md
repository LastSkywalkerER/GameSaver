# 0017 — Procedural UI sound via Web Audio API

Status: accepted
Date: 2026-05-27 (v0.5.0; packs v0.6.2)

## Context
The PS-style UI wants navigation click/select/back sounds, without shipping and managing audio assets.

## Decision
Generate tones at runtime with the Web Audio API (`sound.ts`). Sound **packs**: `psstyle` (default),
`subtle`, `retro` 8-bit, `off`. Choice persists in localStorage; Settings has a picker + preview buttons.

## Consequences
- Zero audio files in the bundle.
- Each pack is a small oscillator recipe for move/select/back.
- Back-compat aliases keep the corner mute toggle working.
