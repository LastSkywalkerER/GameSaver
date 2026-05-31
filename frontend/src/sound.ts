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
// Design: three sine oscillators at a low triad (Cm9-ish — root, fifth,
// minor seventh) plus a soft low-pass + slow LFO on the master gain so
// it breathes instead of holding flat. Master gain caps at ~0.012 — felt
// loud in headphones at 0.02; this is the "you only notice when it
// stops" level the user asked for.
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

type Drone = {
  oscs: OscillatorNode[];
  master: GainNode;
  lfo: OscillatorNode;
  lfoGain: GainNode;
  filter: BiquadFilterNode;
};
let drone: Drone | null = null;

// Chord intervals in Hz — picked to be low enough that even the highest
// voice sits below the speaking voice register, so it doesn't compete
// with anything (e.g. a Discord call) the user might layer on top.
const AMBIENT_CHORD = [110, 164.81, 196]; // A2, E3, G3 — A minor 7 fragment
const AMBIENT_MAX = 0.012;

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
  const now = a.currentTime;

  const master = a.createGain();
  master.gain.setValueAtTime(0.0001, now);
  // 4 s fade-in so turning on doesn't pop on a cold session.
  master.gain.exponentialRampToValueAtTime(AMBIENT_MAX, now + 4);

  const filter = a.createBiquadFilter();
  filter.type = "lowpass";
  filter.frequency.setValueAtTime(900, now); // keep it warm, no sizzle
  filter.Q.setValueAtTime(0.6, now);

  // Slow LFO on the master gain — "breathing" effect. ~0.07 Hz = ~14 s
  // per cycle, slow enough to feel ambient instead of "wobbly".
  const lfo = a.createOscillator();
  lfo.type = "sine";
  lfo.frequency.setValueAtTime(0.07, now);
  const lfoGain = a.createGain();
  lfoGain.gain.setValueAtTime(AMBIENT_MAX * 0.45, now);
  lfo.connect(lfoGain).connect(master.gain);
  lfo.start(now);

  const oscs: OscillatorNode[] = [];
  for (const hz of AMBIENT_CHORD) {
    const o = a.createOscillator();
    o.type = "sine";
    o.frequency.setValueAtTime(hz, now);
    // Tiny per-voice gain so the sum doesn't clip — each voice carries
    // roughly 1/N of the headroom before they recombine in master.
    const og = a.createGain();
    og.gain.setValueAtTime(1 / AMBIENT_CHORD.length, now);
    o.connect(og).connect(filter);
    o.start(now);
    oscs.push(o);
  }
  filter.connect(master).connect(a.destination);

  drone = { oscs, master, lfo, lfoGain, filter };
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

// React to the user turning the sound off in Settings while the drone
// is playing — without this the chord stays running until reload.
subscribeSoundPack((p) => {
  if (p === "off") stopAmbient();
});
