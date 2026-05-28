// MonitorPicker — shown when the user has 2+ active monitors. The window
// is spanned across the WHOLE virtual desktop by the backend
// (PrepareMonitorPick) so we can paint one self-contained picker copy
// centered on EACH physical monitor. That way, no matter which monitor is
// actually lit (e.g. one stayed dark after sleep), the user sees a usable
// picker on a screen they can see.
//
// A single shared selection cursor is driven by controller / keyboard;
// every copy reflects it. Each copy badges the card that corresponds to
// the monitor it's being drawn on ("📍 этот экран") so the user can tell
// which physical screen they're looking at.

import { useEffect, useMemo, useRef, useState } from "react";
import clsx from "clsx";
import { api } from "../../api";
import { useControllerButton, useControllerNav } from "../../controller";
import { playBack, playMove, playSelect } from "../../sound";

export type Monitor = {
  id: string;
  name: string;
  width: number;
  height: number;
  positionX: number;
  positionY: number;
  isPrimary: boolean;
  isEnabled: boolean;
};

export type PickPrep = {
  monitors: Monitor[];
  vx: number; vy: number; vw: number; vh: number;
};

const REMEMBERED_KEY = "gs:soleMonitorId";

function displayLabel(id: string): string {
  const m = id.match(/DISPLAY(\d+)/i);
  return m ? `DISPLAY ${m[1]}` : id;
}

export function MonitorPicker({
  prep,
  onDone,
}: {
  prep: PickPrep;
  // chosenId is the monitor the user committed to (undefined if they
  // skipped). The parent uses it to suppress the self-inflicted
  // display:changed storm that MakeSole triggers.
  onDone: (chosenId?: string) => void;
}) {
  // Sort left-to-right so d-pad direction matches physical layout.
  const monitors = useMemo(
    () => [...prep.monitors].sort((a, b) => a.positionX - b.positionX || a.positionY - b.positionY),
    [prep.monitors],
  );

  // Pre-select the remembered monitor if it's still around, else primary.
  const initial = useMemo(() => {
    let remembered = "";
    try { remembered = localStorage.getItem(REMEMBERED_KEY) || ""; } catch {}
    const ri = monitors.findIndex((m) => m.id === remembered);
    if (ri >= 0) return ri;
    return Math.max(0, monitors.findIndex((m) => m.isPrimary));
  }, [monitors]);

  const [active, setActive] = useState(initial);
  const [applying, setApplying] = useState(false);
  const lastMove = useRef(0);

  function move(delta: number) {
    const now = Date.now();
    if (now - lastMove.current < 120) return;
    lastMove.current = now;
    setActive((i) => {
      const next = Math.max(0, Math.min(monitors.length - 1, i + delta));
      if (next !== i) playMove();
      return next;
    });
  }

  async function confirm() {
    if (applying) return;
    const m = monitors[active];
    if (!m) return;
    playSelect();
    setApplying(true);
    try {
      await api.FinishMonitorPick(m.id);
      try { localStorage.setItem(REMEMBERED_KEY, m.id); } catch {}
      api.Toast("success", `Активный монитор: ${displayLabel(m.id)}`);
      onDone(m.id);
    } catch (e) {
      api.Toast("error", "Не удалось переключить мониторы: " + String(e));
      setApplying(false);
    }
  }

  async function skip() {
    playBack();
    if (applying) return;
    setApplying(true);
    try { await api.CancelMonitorPick(); } catch {}
    onDone();
  }

  useControllerNav((dir) => {
    if (dir === "left")  move(-1);
    if (dir === "right") move(+1);
  });
  useControllerButton((btn) => {
    if (btn === "a") confirm();
    else if (btn === "b" || btn === "back") skip();
  });

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "ArrowLeft") { e.preventDefault(); move(-1); }
      else if (e.key === "ArrowRight") { e.preventDefault(); move(+1); }
      else if (e.key === "Enter") { e.preventDefault(); confirm(); }
      else if (e.key === "Escape") { e.preventDefault(); skip(); }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [active, applying]);

  // Map physical monitor rects into our spanning window's CSS coordinate
  // space. devicePixelRatio handles uniform DPI; mixed-DPI multi-monitor
  // setups will be slightly off but still usable (rare for gaming rigs).
  const dpr = window.devicePixelRatio || 1;

  return (
    <div className="fixed inset-0 z-[2000] bg-black/95">
      {monitors.map((screen) => {
        const left = (screen.positionX - prep.vx) / dpr;
        const top  = (screen.positionY - prep.vy) / dpr;
        const w    = screen.width  / dpr;
        const h    = screen.height / dpr;
        return (
          <div
            key={"copy-" + screen.id}
            className="absolute flex flex-col items-center justify-center p-6 text-center"
            style={{ left, top, width: w, height: h }}
          >
            <div className="mb-1 text-xs uppercase tracking-widest text-emerald-300">
              📍 вы смотрите на {displayLabel(screen.id)}
            </div>
            <h1 className="mb-2 text-3xl font-bold">Выбери активный монитор</h1>
            <p className="mb-6 max-w-md text-sm text-gray-400">
              Остальные выключатся на время shell-сессии. Вернём при выходе.
            </p>

            <div className="flex flex-wrap items-center justify-center gap-3">
              {monitors.map((m, i) => {
                const isActive = i === active;
                const isThis = m.id === screen.id;
                return (
                  <button
                    key={m.id}
                    onClick={() => { if (i !== active) playMove(); setActive(i); }}
                    onDoubleClick={confirm}
                    className={clsx(
                      "flex w-44 flex-col items-center gap-1 rounded-2xl border-2 p-4 transition-all",
                      isActive
                        ? "border-accent bg-accent/20 shadow-[0_10px_40px_rgba(124,92,255,0.5)] scale-105"
                        : "border-white/15 bg-white/5 hover:bg-white/10",
                    )}
                  >
                    <div className="text-base font-bold text-accent">{displayLabel(m.id)}</div>
                    <div className="truncate text-xs font-semibold text-gray-100" style={{ maxWidth: "100%" }}>
                      {m.name}
                    </div>
                    <div className="text-[11px] text-gray-400">
                      {m.width} × {m.height}{m.isPrimary && " · primary"}
                    </div>
                    {isThis && (
                      <div className="mt-1 rounded bg-emerald-700/40 px-2 py-0.5 text-[10px] text-emerald-200">
                        этот экран
                      </div>
                    )}
                  </button>
                );
              })}
            </div>

            <div className="mt-6 flex gap-3">
              <button className="btn" onClick={skip} disabled={applying}>
                Пропустить (B / Esc)
              </button>
              <button className="btn btn-primary" onClick={confirm} disabled={applying}>
                {applying ? "Переключаем…" : "Подтвердить (A / Enter)"}
              </button>
            </div>
          </div>
        );
      })}
    </div>
  );
}
