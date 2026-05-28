// PowerMenu — modal that surfaces "park-the-PC" actions on a big
// controller-friendly grid. Three buttons: Lock, Sleep, Exit shell.
// Navigable via d-pad / arrows; A / Enter confirms; B / Esc closes.

import { useEffect, useRef, useState } from "react";
import clsx from "clsx";
import { api } from "../../api";
import { useControllerButton, useControllerNav } from "../../controller";
import { playBack, playMove, playSelect } from "../../sound";

type Action = {
  key: string;
  label: string;
  icon: string;
  desc: string;
  /** "danger" → red border, used for Exit. */
  variant?: "default" | "danger";
  run: () => void | Promise<void>;
};

export function PowerMenu({
  onClose,
  onExit,
  onSwitchMonitor,
}: {
  onClose: () => void;
  onExit: () => void;
  onSwitchMonitor: () => void;
}) {
  const [active, setActive] = useState(0);
  const lastMove = useRef(0);

  const actions: Action[] = [
    {
      key: "monitor", icon: "🖥", label: "Сменить монитор",
      desc: "Снова покажет выбор активного монитора на всех экранах.",
      run: () => { onClose(); onSwitchMonitor(); },
    },
    {
      key: "lock", icon: "🔒", label: "Заблокировать",
      desc: "Выкинет на экран выбора пользователя. GameSaver продолжит работать в фоне.",
      run: async () => {
        try { await api.LockWorkstation(); onClose(); }
        catch (e) { api.Toast("error", "Lock: " + String(e)); }
      },
    },
    {
      key: "sleep", icon: "🌙", label: "Сон",
      desc: "PC уходит в S3 standby. По нажатию любой клавиши вернётся в работу.",
      run: async () => {
        try { await api.SleepWorkstation(); onClose(); }
        catch (e) { api.Toast("error", "Sleep: " + String(e)); }
      },
    },
    {
      key: "exit", icon: "🛑", label: "Выйти из shell-режима",
      desc: "Снимет регистрацию shell + закроет GameSaver. Watchdog вернёт Explorer.",
      variant: "danger",
      run: () => { onClose(); onExit(); },
    },
  ];

  function move(delta: number) {
    const now = Date.now();
    if (now - lastMove.current < 100) return;
    lastMove.current = now;
    setActive((i) => {
      const next = Math.max(0, Math.min(actions.length - 1, i + delta));
      if (next !== i) playMove();
      return next;
    });
  }

  function confirm() {
    playSelect();
    void actions[active].run();
  }

  useControllerNav((dir) => {
    if (dir === "left")  move(-1);
    if (dir === "right") move(+1);
  });
  useControllerButton((btn) => {
    if (btn === "a") confirm();
    else if (btn === "b" || btn === "back") { playBack(); onClose(); }
  });

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "ArrowLeft") { e.preventDefault(); move(-1); }
      else if (e.key === "ArrowRight") { e.preventDefault(); move(+1); }
      else if (e.key === "Enter") { e.preventDefault(); confirm(); }
      else if (e.key === "Escape") { e.preventDefault(); playBack(); onClose(); }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [active]);

  return (
    <div
      className="fixed inset-0 z-[1500] flex flex-col items-center justify-center bg-black/85 p-8 backdrop-blur-md"
      onClick={onClose}
    >
      <h1 className="mb-10 text-3xl font-semibold text-gray-100">Питание</h1>
      <div className="grid w-full max-w-5xl grid-cols-2 gap-6 md:grid-cols-4" onClick={(e) => e.stopPropagation()}>
        {actions.map((a, i) => {
          const isActive = i === active;
          const danger = a.variant === "danger";
          return (
            <button
              key={a.key}
              onClick={() => { setActive(i); confirm(); }}
              className={clsx(
                "flex flex-col items-center gap-3 rounded-2xl border-2 p-8 text-center transition-all",
                isActive
                  ? danger
                    ? "border-red-500 bg-red-900/30 shadow-[0_10px_40px_rgba(239,68,68,0.45)] scale-105"
                    : "border-accent bg-accent/20 shadow-[0_10px_40px_rgba(124,92,255,0.45)] scale-105"
                  : "border-white/10 bg-white/5 hover:bg-white/10"
              )}
            >
              <div className="text-6xl">{a.icon}</div>
              <div className="text-lg font-semibold text-gray-100">{a.label}</div>
              <div className="text-xs leading-snug text-gray-400">{a.desc}</div>
            </button>
          );
        })}
      </div>
      <div className="mt-10 text-xs text-muted">
        ← → выбор · A / Enter подтвердить · B / Esc закрыть
      </div>
    </div>
  );
}
