// MonitorPicker — full-screen overlay shown at shell-mode startup when
// the user has 2+ active monitors. They pick one with controller / arrows,
// confirm with A/Enter; we then disable the others via the backend
// MakeSoleMonitor call so the shell experience is single-screen like a
// console. The user's choice is remembered (gs:soleMonitorId localStorage)
// so subsequent logons skip this overlay automatically.

import { useEffect, useRef, useState } from "react";
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

const REMEMBERED_KEY = "gs:soleMonitorId";

export function MonitorPicker({
  monitors,
  onDone,
}: {
  monitors: Monitor[];
  onDone: () => void;
}) {
  // Start on the primary monitor if we have one — that's almost always
  // where the user is looking when shell mode boots.
  const initial = Math.max(0, monitors.findIndex((m) => m.isPrimary));
  const [active, setActive] = useState(initial);
  const [applying, setApplying] = useState(false);
  const lastMove = useRef(0);

  function move(delta: number) {
    // Soft debounce so a held d-pad or arrow doesn't fly past everything.
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
      await api.MakeSoleMonitor(m.id);
      try { localStorage.setItem(REMEMBERED_KEY, m.id); } catch {}
      api.Toast("success", `Активный монитор: ${m.name}`);
      onDone();
    } catch (e) {
      api.Toast("error", "Не удалось переключить мониторы: " + String(e));
      setApplying(false);
    }
  }

  function skip() {
    playBack();
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

  // Layout: each monitor is a card sized proportional to its real
  // resolution, positioned roughly where it lives in the desktop topology.
  // Visualises e.g. "primary on the left, vertical secondary on the right".
  const minX = Math.min(...monitors.map((m) => m.positionX));
  const minY = Math.min(...monitors.map((m) => m.positionY));
  const maxX = Math.max(...monitors.map((m) => m.positionX + m.width));
  const maxY = Math.max(...monitors.map((m) => m.positionY + m.height));
  const span = Math.max(maxX - minX, 1);
  const spanY = Math.max(maxY - minY, 1);
  const PX_W = 600; // canvas width
  const scale = PX_W / span;

  return (
    <div className="fixed inset-0 z-[2000] flex flex-col items-center justify-center bg-black/95 p-10 backdrop-blur-md">
      <h1 className="mb-3 text-4xl font-bold">Выбери активный монитор</h1>
      <p className="mb-10 max-w-xl text-center text-gray-400">
        Остальные мониторы будут отключены на время shell-сессии. Восстановим,
        как только выйдешь из shell-режима.
      </p>

      <div
        className="relative mb-10"
        style={{ width: PX_W, height: spanY * scale + 40 }}
      >
        {monitors.map((m, i) => {
          const left = (m.positionX - minX) * scale;
          const top  = (m.positionY - minY) * scale;
          const w    = m.width  * scale;
          const h    = m.height * scale;
          const isActive = i === active;
          return (
            <button
              key={m.id}
              onClick={() => { if (i !== active) playMove(); setActive(i); }}
              onDoubleClick={confirm}
              className={clsx(
                "absolute flex flex-col items-center justify-center rounded-2xl border-4 p-3 text-center transition-all",
                isActive
                  ? "border-accent bg-accent/20 shadow-[0_10px_40px_rgba(124,92,255,0.5)] scale-105"
                  : "border-white/15 bg-white/5 hover:bg-white/10"
              )}
              style={{ left, top, width: w, height: h }}
            >
              <div className="text-sm font-semibold text-gray-100">{m.name}</div>
              <div className="text-xs text-gray-400">
                {m.width} × {m.height}
                {m.isPrimary && " · primary"}
              </div>
            </button>
          );
        })}
      </div>

      <div className="flex gap-3">
        <button className="btn" onClick={skip} disabled={applying}>
          Пропустить (Esc / B)
        </button>
        <button className="btn btn-primary" onClick={confirm} disabled={applying}>
          {applying ? "Переключаем…" : "Подтвердить (Enter / A)"}
        </button>
      </div>
    </div>
  );
}
