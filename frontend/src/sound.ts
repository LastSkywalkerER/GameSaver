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
