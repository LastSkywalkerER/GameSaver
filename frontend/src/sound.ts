// Procedural nav tones via Web Audio API + a looping MP3 ambient pad.
//
// v0.10.1 simplification:
//   - The sound-pack picker is gone. Nav tones are pinned to the
//     "subtle" voice (lowest-impact, sine-based) per the user brief.
//     The only user-facing knob is on/off, stored in localStorage as
//     gs:soundOn (boolean). The old "gs:soundPack" key is migrated on
//     read.
//   - The procedural ambient drone is replaced by a real audio file
//     (assets/ambient.mp3 — embedded by Vite). We decode it once,
//     trim the first/last 15 s (the brief: "обрежь первые и
//     последние 15 секунд"), then bake a seamlessly-looping
//     AudioBuffer by crossfading the tail into the head ("максимально
//     плавное перетекание от конца к началу"). source.loop = true on
//     that buffer gives a continuous, click-free pad.

import ambientMp3 from "./assets/ambient.mp3";

const SOUND_ON_KEY = "gs:soundOn";
const LEGACY_PACK_KEY = "gs:soundPack";

// Kept exported as a no-op compatibility shim so callers that used to
// read getSoundPack() / subscribeSoundPack() compile without rewrites
// during the v0.10.1 transition. New code should read isNavSoundEnabled
// and subscribe via subscribeNavSound.
export type SoundPack = "subtle" | "off";
export const SOUND_PACK_LABELS: Record<SoundPack, string> = {
  subtle: "Тихий",
  off:    "Выключено",
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

let enabled: boolean = (() => {
  try {
    // Honour the new key first.
    const raw = localStorage.getItem(SOUND_ON_KEY);
    if (raw === "true") return true;
    if (raw === "false") return false;
    // Migrate from v0.9.x SoundPack: "off" -> false, anything else -> true.
    const legacy = localStorage.getItem(LEGACY_PACK_KEY);
    if (legacy === "off") return false;
  } catch {}
  return true;
})();

const onSubs = new Set<(on: boolean) => void>();
export function isNavSoundEnabled(): boolean { return enabled; }
export function setNavSoundEnabled(v: boolean) {
  enabled = v;
  try { localStorage.setItem(SOUND_ON_KEY, String(v)); } catch {}
  onSubs.forEach((fn) => fn(v));
}
export function subscribeNavSound(fn: (on: boolean) => void): () => void {
  onSubs.add(fn);
  return () => { onSubs.delete(fn); };
}

// ─── Compatibility shims (will be removed once callers migrate) ───────
export function getSoundPack(): SoundPack { return enabled ? "subtle" : "off"; }
export function setSoundPack(p: SoundPack) { setNavSoundEnabled(p !== "off"); }
export function subscribeSoundPack(fn: (p: SoundPack) => void): () => void {
  const wrap = (on: boolean) => fn(on ? "subtle" : "off");
  onSubs.add(wrap);
  return () => { onSubs.delete(wrap); };
}

// ─── Nav tones — "subtle" voice, hardcoded ─────────────────────────────

type Tone = { type: OscillatorType; from: number; to: number; gain: number; ms: number; delay?: number };

const TONE_MOVE:   Tone[] = [{ type: "sine", from: 520, to: 380, gain: 0.05, ms: 70 }];
const TONE_SELECT: Tone[] = [{ type: "sine", from: 440, to: 660, gain: 0.07, ms: 140 }];
const TONE_BACK:   Tone[] = [{ type: "sine", from: 300, to: 200, gain: 0.05, ms: 90 }];

function play(tones: Tone[]) {
  if (!enabled) return;
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

export function playMove()   { play(TONE_MOVE); }
export function playSelect() { play(TONE_SELECT); }
export function playBack()   { play(TONE_BACK); }

// ─── Ambient — looped MP3 with trim + seamless crossfade ───────────────
//
// The decode + crossfade-bake is async and only runs once per session.
// startAmbient() schedules a play after the buffer is ready; if the
// user toggles sound off while we're still fetching, the play is
// cancelled by the same `enabled` check.

const TRIM_HEAD_SEC = 15;   // brief: drop first 15 s
const TRIM_TAIL_SEC = 15;   // brief: drop last 15 s
const XFADE_SEC = 6;        // tail→head crossfade window for the loop
const AMBIENT_GAIN = 0.10;  // master cap. MP3 is mastered louder than the
                            // procedural drone (which sat at 0.011); this
                            // brings the felt level into the same "barely
                            // there" range.

let loopBuffer: AudioBuffer | null = null;
let loopBufferPromise: Promise<AudioBuffer> | null = null;
let curSource: AudioBufferSourceNode | null = null;
let curGain: GainNode | null = null;
let stoppedFlag = false;

export function isAmbientOn(): boolean { return curSource !== null; }

async function loadAndBakeLoop(a: AudioContext): Promise<AudioBuffer> {
  if (loopBuffer) return loopBuffer;
  if (loopBufferPromise) return loopBufferPromise;
  loopBufferPromise = (async () => {
    const resp = await fetch(ambientMp3);
    const raw = await resp.arrayBuffer();
    const decoded = await a.decodeAudioData(raw);

    const sr = decoded.sampleRate;
    const totalLen = decoded.length;
    const headCut = Math.min(Math.floor(TRIM_HEAD_SEC * sr), Math.floor(totalLen / 4));
    const tailCut = Math.min(Math.floor(TRIM_TAIL_SEC * sr), Math.floor(totalLen / 4));
    const trimmedLen = Math.max(1, totalLen - headCut - tailCut);
    // crossfade window can't exceed a third of the trimmed body or the
    // overlap math collapses; clamp generously.
    const W = Math.min(Math.floor(XFADE_SEC * sr), Math.floor(trimmedLen / 3));

    // Final loop buffer is (trimmed - W) samples long. When source.loop
    // is true, sample [N-1] → sample [0] gives a seamless original-
    // timeline continuation because we've baked the original tail into
    // the start of the buffer via a sin² equal-power crossfade:
    //
    //   out[i, i<W]  = original_head[i] * sin(t·π/2) +
    //                  original_tail[i] * cos(t·π/2)   where t = i / W
    //   out[i, i>=W] = original_head[i]
    //
    // The "original_head" run from sample headCut → headCut + (trimmedLen - W).
    // The "original_tail" run is the LAST W samples of the trimmed region,
    // i.e. original[headCut + trimmedLen - W .. headCut + trimmedLen].
    //
    // sin²+cos² = 1 keeps the perceived loudness flat through the blend.
    const outLen = trimmedLen - W;
    const out = a.createBuffer(decoded.numberOfChannels, outLen, sr);
    for (let ch = 0; ch < decoded.numberOfChannels; ch++) {
      const inData = decoded.getChannelData(ch);
      const outData = out.getChannelData(ch);
      const headBase = headCut;
      const tailBase = headCut + trimmedLen - W;
      for (let i = 0; i < outLen; i++) {
        const headSample = inData[headBase + i] || 0;
        if (i < W) {
          const t = i / W;
          const tailSample = inData[tailBase + i] || 0;
          // Equal-power crossfade: head fades IN, tail fades OUT.
          const headK = Math.sin((t * Math.PI) / 2);
          const tailK = Math.cos((t * Math.PI) / 2);
          outData[i] = headSample * headK + tailSample * tailK;
        } else {
          outData[i] = headSample;
        }
      }
    }
    loopBuffer = out;
    return out;
  })().catch((e) => {
    console.warn("ambient: load/bake failed", e);
    loopBufferPromise = null;
    throw e;
  });
  return loopBufferPromise;
}

export function startAmbient() {
  if (!enabled) return;
  if (curSource) return;
  const a = audio(); if (!a) return;
  if (a.state === "suspended") a.resume().catch(() => {});
  stoppedFlag = false;
  loadAndBakeLoop(a).then((buf) => {
    if (stoppedFlag || !enabled) return;
    if (curSource) return;
    const src = a.createBufferSource();
    src.buffer = buf;
    src.loop = true;
    const g = a.createGain();
    const now = a.currentTime;
    g.gain.setValueAtTime(0.0001, now);
    // 4 s fade-in matches the procedural drone's old envelope so the
    // pad doesn't pop on cold-start.
    g.gain.exponentialRampToValueAtTime(AMBIENT_GAIN, now + 4);
    src.connect(g).connect(a.destination);
    src.start();
    curSource = src;
    curGain = g;
  }).catch(() => {});
}

export function stopAmbient() {
  stoppedFlag = true;
  if (!curSource || !curGain) return;
  const a = audio(); if (!a) {
    try { curSource.stop(); } catch {}
    curSource = null; curGain = null;
    return;
  }
  const now = a.currentTime;
  const src = curSource;
  const g = curGain;
  curSource = null; curGain = null;
  // 1.5 s fade-out before tearing the source down — same envelope as
  // the v0.9 procedural drone.
  g.gain.cancelScheduledValues(now);
  g.gain.setValueAtTime(g.gain.value, now);
  g.gain.exponentialRampToValueAtTime(0.0001, now + 1.5);
  g.gain.setValueAtTime(0, now + 1.55);
  setTimeout(() => {
    try { src.stop(); } catch {}
    try { src.disconnect(); } catch {}
    try { g.disconnect(); } catch {}
  }, 1700);
}

// Turning sound off in Settings kills the ambient immediately.
subscribeNavSound((on) => {
  if (!on) stopAmbient();
});
