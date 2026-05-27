// Procedural UI sounds via Web Audio API — no asset files to ship.
//
// Shell mode UI calls these on tile-to-tile navigation and on the Play
// button to give the PS-style audible feedback. AudioContext can only be
// constructed after a user gesture in modern browsers, so the first move
// in shell mode usually arrives via the controller — Wails' WebView2 is
// permissive enough that we can construct it eagerly here.

const NAV_SOUND_KEY = "gs:navSoundOn";

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

let enabled = (() => {
  try { return localStorage.getItem(NAV_SOUND_KEY) !== "0"; } catch { return true; }
})();
export function setNavSoundEnabled(v: boolean) {
  enabled = v;
  try { localStorage.setItem(NAV_SOUND_KEY, v ? "1" : "0"); } catch {}
}
export function isNavSoundEnabled(): boolean { return enabled; }

/**
 * Short crisp tick used when the carousel cursor moves to the next tile.
 * Falls between PS5's "subtle whoosh" and Switch's "click". ~80 ms total.
 */
export function playMove() {
  if (!enabled) return;
  const a = audio(); if (!a) return;
  const t = a.currentTime;
  const osc = a.createOscillator();
  const gain = a.createGain();
  osc.type = "sine";
  osc.frequency.setValueAtTime(880, t);
  osc.frequency.exponentialRampToValueAtTime(540, t + 0.08);
  gain.gain.setValueAtTime(0.0001, t);
  gain.gain.exponentialRampToValueAtTime(0.10, t + 0.005);
  gain.gain.exponentialRampToValueAtTime(0.0001, t + 0.09);
  osc.connect(gain).connect(a.destination);
  osc.start(t);
  osc.stop(t + 0.1);
}

/**
 * "Select" — A button. Two-tone confirmation that the action took.
 */
export function playSelect() {
  if (!enabled) return;
  const a = audio(); if (!a) return;
  const t = a.currentTime;
  const tones = [
    { f: 660, dt: 0.0 },
    { f: 990, dt: 0.06 },
  ];
  for (const { f, dt } of tones) {
    const osc = a.createOscillator();
    const gain = a.createGain();
    osc.type = "triangle";
    osc.frequency.setValueAtTime(f, t + dt);
    gain.gain.setValueAtTime(0.0001, t + dt);
    gain.gain.exponentialRampToValueAtTime(0.14, t + dt + 0.005);
    gain.gain.exponentialRampToValueAtTime(0.0001, t + dt + 0.12);
    osc.connect(gain).connect(a.destination);
    osc.start(t + dt);
    osc.stop(t + dt + 0.13);
  }
}

/**
 * "Back" — B button. Lower pitch, dampened.
 */
export function playBack() {
  if (!enabled) return;
  const a = audio(); if (!a) return;
  const t = a.currentTime;
  const osc = a.createOscillator();
  const gain = a.createGain();
  osc.type = "sine";
  osc.frequency.setValueAtTime(440, t);
  osc.frequency.exponentialRampToValueAtTime(220, t + 0.1);
  gain.gain.setValueAtTime(0.0001, t);
  gain.gain.exponentialRampToValueAtTime(0.10, t + 0.005);
  gain.gain.exponentialRampToValueAtTime(0.0001, t + 0.12);
  osc.connect(gain).connect(a.destination);
  osc.start(t);
  osc.stop(t + 0.13);
}
