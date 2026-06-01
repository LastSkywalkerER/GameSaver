// VolumePicker — full-screen volume slider for the gamepad.
//
// Backend talks to the default render endpoint via IAudioEndpointVolume.
// We can't absolute-set the level from Go without inline assembly (the
// float arg goes to XMM1, which syscall.SyscallN can't target), so the
// API is step-based: VolumeStepUp / VolumeStepDown / SetMute / Get. One
// step is the driver's natural increment (typically 2% of the dB range).
//
// Controller bindings:
//   ← / →         ±1 step
//   LB / RB       ±5 steps (faster scrub)
//   ↑             unmute (also fired automatically before a step-up if
//                 the user starts adjusting while muted — same affordance
//                 as the volume keys on a keyboard)
//   ↓             mute
//   A             toggle mute
//   B / Esc       close
//
// The visual is a 21-segment horizontal bar reading current step/stepCount.
// Updates after every action so it stays in sync with the OS (other
// volume changes — Windows tray, media keys — won't reflect until the
// next reopen, but the snapshot is enough for the typical "open, set,
// close" flow).

import { useEffect, useRef, useState } from "react";
import clsx from "clsx";
import { api } from "../../api";
import { useControllerButton, useControllerNav } from "../../controller";
import { playBack, playMove, playSelect } from "../../sound";

type Vol = {
  level: number;       // 0.0..1.0
  muted: boolean;
  step: number;
  stepCount: number;
};

export function VolumePicker({ onDone }: { onDone: () => void }) {
  const [v, setV] = useState<Vol | null>(null);
  const [busy, setBusy] = useState(false);
  const lastMove = useRef(0);

  async function refresh() {
    try {
      const out: any = await (api as any).GetVolume();
      if (out) setV({
        level: Number(out.level) || 0,
        muted: !!out.muted,
        step: Number(out.step) || 0,
        stepCount: Number(out.stepCount) || 50,
      });
    } catch (e) {
      api.Toast("error", "Громкость: " + String(e));
    }
  }
  useEffect(() => { refresh(); }, []);

  async function step(delta: number) {
    if (busy) return;
    const now = Date.now();
    if (now - lastMove.current < 80) return;
    lastMove.current = now;
    setBusy(true);
    playMove();
    try {
      if (delta > 0) await (api as any).VolumeStepUp(delta);
      else await (api as any).VolumeStepDown(-delta);
      await refresh();
    } catch (e) {
      api.Toast("error", "Step: " + String(e));
    } finally { setBusy(false); }
  }

  async function toggleMute() {
    if (busy || !v) return;
    setBusy(true);
    playSelect();
    try {
      await (api as any).SetVolumeMute(!v.muted);
      await refresh();
    } catch (e) {
      api.Toast("error", "Mute: " + String(e));
    } finally { setBusy(false); }
  }

  useControllerNav((dir) => {
    if (dir === "left")  step(-1);
    else if (dir === "right") step(+1);
    else if (dir === "up")    {
      // Up = "unmute" — discoverable shortcut matching the keyboard's
      // VK_VOLUME_UP behaviour (which also unmutes implicitly).
      if (v?.muted) toggleMute();
    }
    else if (dir === "down")  { if (!v?.muted) toggleMute(); }
  });
  useControllerButton((btn) => {
    if (btn === "a") toggleMute();
    else if (btn === "lb") step(-5);
    else if (btn === "rb") step(+5);
    else if (btn === "b" || btn === "back") { playBack(); onDone(); }
  });

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "ArrowLeft")  { e.preventDefault(); step(-1); }
      else if (e.key === "ArrowRight") { e.preventDefault(); step(+1); }
      else if (e.key === "ArrowUp")    { e.preventDefault(); if (v?.muted) toggleMute(); }
      else if (e.key === "ArrowDown")  { e.preventDefault(); if (!v?.muted) toggleMute(); }
      else if (e.key === "Enter" || e.key.toLowerCase() === "m") { e.preventDefault(); toggleMute(); }
      else if (e.key === "Escape") { e.preventDefault(); playBack(); onDone(); }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [v, busy]);

  // Visual: split the bar into 21 segments. Each segment lights up if
  // its threshold ≤ current level. Picked 21 because it gives a useful
  // 5% granularity feel even though the actual driver step is ~2%.
  const SEGMENTS = 21;
  const filled = v && !v.muted ? Math.round(v.level * SEGMENTS) : 0;
  const pct = v ? Math.round(v.level * 100) : 0;

  return (
    <div
      className="fixed inset-0 z-[2000] flex flex-col items-center justify-center bg-black/90 p-8 backdrop-blur-md"
      onClick={onDone}
    >
      <div
        className="flex w-full max-w-3xl flex-col items-center gap-8"
        onClick={(e) => e.stopPropagation()}
      >
        <h1 className="text-3xl font-semibold text-gray-100">
          {v?.muted ? "🔇 Звук отключён" : "🔊 Громкость"}
        </h1>

        <div className="w-full">
          <div className="mb-3 flex items-baseline justify-between text-sm text-muted">
            <span>0%</span>
            <span className={v?.muted ? "text-red-300" : "text-gray-200"}>
              {v?.muted ? "MUTE" : `${pct}%`}
            </span>
            <span>100%</span>
          </div>
          <div className="flex h-12 w-full items-stretch gap-1 rounded-2xl border border-white/10 bg-white/5 p-2">
            {Array.from({ length: SEGMENTS }, (_, i) => {
              const isLit = i < filled;
              // The trailing few segments use a warmer hue so the user
              // notices when they're heading toward "too loud".
              const hot = i >= SEGMENTS - 4;
              return (
                <div
                  key={i}
                  className={clsx(
                    "flex-1 rounded-md transition-all",
                    isLit
                      ? hot
                        ? "bg-amber-400/90"
                        : "bg-accent/90"
                      : "bg-white/5"
                  )}
                />
              );
            })}
          </div>
          <div className="mt-3 text-center text-xs text-muted">
            Шаг ≈ {v ? (100 / Math.max(1, v.stepCount)).toFixed(1) : "2"}% (от драйвера)
            {v ? ` · ${v.step}/${v.stepCount}` : ""}
          </div>
        </div>

        <div className="flex flex-wrap items-center justify-center gap-3">
          <button className="btn" disabled={busy} onClick={() => step(-5)}>≪ −5 (LB)</button>
          <button className="btn" disabled={busy} onClick={() => step(-1)}>← −1</button>
          <button
            className={clsx("btn", v?.muted && "btn-primary")}
            disabled={busy}
            onClick={toggleMute}
          >
            {v?.muted ? "🔊 Включить" : "🔇 Mute"} (A)
          </button>
          <button className="btn" disabled={busy} onClick={() => step(+1)}>+1 →</button>
          <button className="btn" disabled={busy} onClick={() => step(+5)}>+5 ≫ (RB)</button>
        </div>

        <div className="text-xs text-muted">
          ← → шаг · LB / RB крупный шаг · A — Mute · B / Esc — закрыть
        </div>

        <button className="btn" onClick={() => { playBack(); onDone(); }}>
          Закрыть (B / Esc)
        </button>
      </div>
    </div>
  );
}
