// Procedural UI sounds via Web Audio API — no asset files to ship.
// Shell mode plays one of these on every controller / keyboard / mouse
// navigation event. Multiple presets let the user pick a vibe (or off).

const SOUND_PACK_KEY = "gs:soundPack";

export type SoundPack = "psstyle" | "subtle" | "retro" | "off";

export const SOUND_PACK_LABELS: Record<SoundPack, string> = {
  psstyle: "PS-style (по умолчанию)",
  subtle:  "Тихий",
  retro:   "Retro 8-bit",
  off:     "Выключено",
};

let ctx: AudioContext | null = null;
function audio(): AudioContext | null {
  if (ctx) return ctx;
  try {
    const Ctor = (window as any).AudioContext || (window as any).webkitAudioContext;
    if (!Ctor) return null;
    ctx = new Ctor();
    return ctx;
  } catch { return null; }
}

let pack: SoundPack = (() => {
  try {
    const v = localStorage.getItem(SOUND_PACK_KEY) as SoundPack | null;
    if (v === "psstyle" || v === "subtle" || v === "retro" || v === "off") return v;
  } catch {}
  return "psstyle";
})();

export function getSoundPack(): SoundPack { return pack; }

const packSubs = new Set<(p: SoundPack) => void>();
export function setSoundPack(p: SoundPack) {
  pack = p;
  try { localStorage.setItem(SOUND_PACK_KEY, p); } catch {}
  packSubs.forEach((fn) => fn(p));
}
export function subscribeSoundPack(fn: (p: SoundPack) => void): () => void {
  packSubs.add(fn);
  return () => { packSubs.delete(fn); };
}

// Back-compat with v0.5.x callers — keeps the mute toggle in CornerIcons
// working. "Enabled" == any pack other than "off"; flipping it on returns
// to the default pack rather than the previously-selected non-off one,
// which is fine because v0.6.2+ users will use the new sound picker.
export function isNavSoundEnabled(): boolean { return pack !== "off"; }
export function setNavSoundEnabled(v: boolean) { setSoundPack(v ? "psstyle" : "off"); }

// ─── Pack definitions ──────────────────────────────────────────────────

type Tone = { type: OscillatorType; from: number; to: number; gain: number; ms: number; delay?: number };

const PACK_MOVE: Record<Exclude<SoundPack, "off">, Tone[]> = {
  psstyle: [{ type: "sine",     from: 880, to: 540, gain: 0.10, ms: 90 }],
  subtle:  [{ type: "sine",     from: 520, to: 380, gain: 0.05, ms: 70 }],
  retro:   [{ type: "square",   from: 660, to: 660, gain: 0.08, ms: 50 }],
};

const PACK_SELECT: Record<Exclude<SoundPack, "off">, Tone[]> = {
  psstyle: [
    { type: "triangle", from: 660, to: 660, gain: 0.14, ms: 120 },
    { type: "triangle", from: 990, to: 990, gain: 0.14, ms: 120, delay: 60 },
  ],
  subtle:  [{ type: "sine",     from: 440, to: 660, gain: 0.07, ms: 140 }],
  retro:   [
    { type: "square",   from: 523, to: 523, gain: 0.10, ms: 70 },
    { type: "square",   from: 784, to: 784, gain: 0.10, ms: 70, delay: 70 },
    { type: "square",  from: 1047, to: 1047, gain: 0.10, ms: 100, delay: 140 },
  ],
};

const PACK_BACK: Record<Exclude<SoundPack, "off">, Tone[]> = {
  psstyle: [{ type: "sine",     from: 440, to: 220, gain: 0.10, ms: 120 }],
  subtle:  [{ type: "sine",     from: 300, to: 200, gain: 0.05, ms: 90 }],
  retro:   [{ type: "square",   from: 392, to: 220, gain: 0.10, ms: 120 }],
};

function play(tones: Tone[]) {
  if (pack === "off") return;
  const a = audio(); if (!a) return;
  const t0 = a.currentTime;
  for (const tn of tones) {
    const start = t0 + (tn.delay ?? 0) / 1000;
    const dur = tn.ms / 1000;
    const osc = a.createOscillator();
    const gain = a.createGain();
    osc.type = tn.type;
    osc.frequency.setValueAtTime(tn.from, start);
    if (tn.to !== tn.from) {
      osc.frequency.exponentialRampToValueAtTime(tn.to, start + dur);
    }
    gain.gain.setValueAtTime(0.0001, start);
    gain.gain.exponentialRampToValueAtTime(tn.gain, start + 0.005);
    gain.gain.exponentialRampToValueAtTime(0.0001, start + dur);
    osc.connect(gain).connect(a.destination);
    osc.start(start);
    osc.stop(start + dur + 0.02);
  }
}

export function playMove()   { if (pack !== "off") play(PACK_MOVE[pack]); }
export function playSelect() { if (pack !== "off") play(PACK_SELECT[pack]); }
export function playBack()   { if (pack !== "off") play(PACK_BACK[pack]); }

// ─── Ambient drone ─────────────────────────────────────────────────────
//
// A very quiet procedural pad that plays while the shell-mode menu is
// visible — turn the screen on, hear a soft breathing chord. Same Web
// Audio API as the navigation tones (no asset files per the rules).
//
// v0.9.3: the v0.9.2 default was an A-minor-7 fragment — users said it
// read as gloomy. Replaced with a major add9 (Cmaj9-ish), and added
// per-pack variants so the existing Settings → "Звук навигации"
// selector also picks the drone flavour:
//
//   psstyle — bright warm pad (Cmaj9, sine, breathing LFO). Default.
//   subtle  — sparse open fifth, very quiet, the "you only notice when
//             it stops" target.
//   retro   — 8-bit-ish boot drone: triangle thirds, a touch of detune,
//             slightly faster LFO so it has movement.
//   off     — silent (matches the navigation tones being off).
//
// Master gain caps stay around 0.012 — felt loud in headphones at 0.02;
// this is the level the user asked for.
//
// Lifecycle:
//   start() — called when the shell becomes active AND audio is allowed
//             (AudioContext autoplay requires a prior user gesture; we
//             call start() lazily from within a click/keydown handler).
//   stop()  — called on visibility hidden / window minimize (game
//             launched) / pack === "off". Smoothly ramps to silence and
//             tears down the oscillators so we're not burning CPU while
//             the user is in a game.
//
// We expose isAmbientOn() so the corner icon can show a state.

type AmbientConfig = {
  // Chord voicing in Hz. Kept low — even the highest voice sits below
  // the speaking-voice register so the pad doesn't fight a Discord call
  // layered on top of the menu.
  chord: number[];
  // Per-voice detune in cents, applied alternately +/- so the chord
  // gets a tiny chorus-y shimmer. 0 = perfectly pure (best for very
  // sparse voicings; chorusing two voices reads as a beat instead).
  detuneCents: number;
  oscType: OscillatorType;
  // Master low-pass cutoff (Hz) — warmer the lower it is.
  filterCutHz: number;
  filterQ: number;
  // Peak master gain at the apex of the LFO breath. 0.012 ≈ "ambient".
  maxGain: number;
  // LFO frequency for the gain breath. ~0.06-0.12 Hz = 8-16 s per cycle.
  lfoHz: number;
  // Fraction of maxGain the LFO swings — 0.45 means gain breathes
  // between roughly 55% and 100% of peak.
  lfoDepth: number;
};

const AMBIENT_PACKS: Record<Exclude<SoundPack, "off">, AmbientConfig> = {
  // v0.10.0: dropped the C3 root from the Cmaj9 voicing — that low note
  // was what made the pad feel "давящий" (oppressive). The remaining
  // upper-structure (E3-G3-B3-D4) reads as bright and airy, like the
  // top half of a Cmaj9 floating without weight underneath. Master
  // gain pulled down a touch too. Filter sits where it did so it
  // stays warm rather than glassy.
  psstyle: {
    chord: [164.81, 196.00, 246.94, 293.66],
    detuneCents: 4,
    oscType: "sine",
    filterCutHz: 1300,
    filterQ: 0.5,
    maxGain: 0.011,
    lfoHz: 0.07,
    lfoDepth: 0.40,
  },
  // Sparse upper-fifth pad: G3 + D4. Two sine voices, no detune
  // (chorusing two close pitches reads as a beat, not shimmer). The
  // "you only notice when it stops" target.
  subtle: {
    chord: [196.00, 293.66],
    detuneCents: 0,
    oscType: "sine",
    filterCutHz: 800,
    filterQ: 0.5,
    maxGain: 0.007,
    lfoHz: 0.05,
    lfoDepth: 0.50,
  },
  // 8-bit boot vibe — C major triad voiced UP an octave (C4-E4-G4)
  // instead of root-position. Bright triangle voices, gentle detune,
  // brighter LPF. Feels more "menu screen" than "ambient room".
  retro: {
    chord: [261.63, 329.63, 392.00],
    detuneCents: 5,
    oscType: "triangle",
    filterCutHz: 2000,
    filterQ: 0.4,
    maxGain: 0.009,
    lfoHz: 0.10,
    lfoDepth: 0.55,
  },
};

type Drone = {
  oscs: OscillatorNode[];
  master: GainNode;
  lfo: OscillatorNode;
  lfoGain: GainNode;
  filter: BiquadFilterNode;
  packAtStart: SoundPack;
};
let drone: Drone | null = null;

export function isAmbientOn(): boolean { return drone !== null; }

export function startAmbient() {
  if (drone || pack === "off") return;
  const a = audio(); if (!a) return;
  // Resume the context if it was suspended by the browser autoplay gate
  // — caller is responsible for invoking start() from a user gesture but
  // we resume defensively in case the context was created earlier.
  if (a.state === "suspended") {
    a.resume().catch(() => { /* will retry next gesture */ });
  }
  const cfg = AMBIENT_PACKS[pack as Exclude<SoundPack, "off">];
  const now = a.currentTime;

  const master = a.createGain();
  master.gain.setValueAtTime(0.0001, now);
  // 4 s fade-in so turning on doesn't pop on a cold session.
  master.gain.exponentialRampToValueAtTime(cfg.maxGain, now + 4);

  const filter = a.createBiquadFilter();
  filter.type = "lowpass";
  filter.frequency.setValueAtTime(cfg.filterCutHz, now);
  filter.Q.setValueAtTime(cfg.filterQ, now);

  // LFO on master gain — "breathing" effect. Slow enough to feel
  // ambient instead of wobbly (~8-16 s per cycle depending on pack).
  const lfo = a.createOscillator();
  lfo.type = "sine";
  lfo.frequency.setValueAtTime(cfg.lfoHz, now);
  const lfoGain = a.createGain();
  lfoGain.gain.setValueAtTime(cfg.maxGain * cfg.lfoDepth, now);
  lfo.connect(lfoGain).connect(master.gain);
  lfo.start(now);

  const oscs: OscillatorNode[] = [];
  cfg.chord.forEach((hz, i) => {
    const o = a.createOscillator();
    o.type = cfg.oscType;
    o.frequency.setValueAtTime(hz, now);
    // Alternate detune sign per voice for a stereo-less chorus shimmer.
    // No-op when cfg.detuneCents == 0.
    o.detune.setValueAtTime((i % 2 === 0 ? +1 : -1) * cfg.detuneCents, now);
    // Tiny per-voice gain so the sum doesn't clip — each voice carries
    // roughly 1/N of the headroom before they recombine in master.
    const og = a.createGain();
    og.gain.setValueAtTime(1 / cfg.chord.length, now);
    o.connect(og).connect(filter);
    o.start(now);
    oscs.push(o);
  });
  filter.connect(master).connect(a.destination);

  drone = { oscs, master, lfo, lfoGain, filter, packAtStart: pack };
}

export function stopAmbient() {
  if (!drone) return;
  const a = audio(); if (!a) { drone = null; return; }
  const now = a.currentTime;
  const d = drone;
  drone = null; // mark stopped immediately so re-entrant calls are no-ops

  // 1.5 s exponential fade-out then tear-down. exponentialRampToValueAtTime
  // can't hit zero so we step to near-zero, then setValueAtTime to 0 a
  // moment later to fully silence the nodes before .stop().
  d.master.gain.cancelScheduledValues(now);
  d.master.gain.setValueAtTime(d.master.gain.value, now);
  d.master.gain.exponentialRampToValueAtTime(0.0001, now + 1.5);
  d.master.gain.setValueAtTime(0, now + 1.55);

  const teardown = () => {
    try { d.oscs.forEach((o) => o.stop()); } catch {}
    try { d.lfo.stop(); } catch {}
    try {
      d.oscs.forEach((o) => o.disconnect());
      d.lfoGain.disconnect();
      d.lfo.disconnect();
      d.filter.disconnect();
      d.master.disconnect();
    } catch {}
  };
  setTimeout(teardown, 1700);
}

// React to pack changes:
//   off               — silence immediately;
//   any other change  — restart so the new chord/colour takes over.
// stopAmbient + startAmbient will crossfade nicely (1.5 s out + 4 s in),
// so the swap is musical rather than a hard cut.
subscribeSoundPack((p) => {
  if (p === "off") { stopAmbient(); return; }
  if (drone && drone.packAtStart !== p) {
    stopAmbient();
    // Wait long enough for the existing fade-out to clear before
    // bringing up the new voices — otherwise both chords overlap at
    // peak for ~1 s and the level briefly doubles.
    setTimeout(() => startAmbient(), 1700);
  }
});
