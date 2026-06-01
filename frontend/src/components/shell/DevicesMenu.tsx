// DevicesMenu — the "what do you want to configure" splash before
// dropping into one of the device pickers. Shares PowerMenu's layout
// so the controller / mouse experience reads as a single family of
// modals (translucent tiles on a blurred backdrop, ←→ + A/B nav).
//
// Reachable from:
//   - 🎛 corner icon on the shell home screen (single button that
//     replaces the v0.9 monitor + audio pair of icons);
//   - Back / Select on the controller (which used to open Backups,
//     re-bound in v0.10.0 to be the device chooser since picking a
//     monitor / output is far more frequent than browsing backups).
//
// We delegate to the existing pickers — this component owns nothing
// past "which one did the user click". The Bluetooth tile is a stub
// in v0.10.0; the actual picker lands in v0.10.1.

import { useEffect, useRef, useState } from "react";
import clsx from "clsx";
import { useControllerButton, useControllerNav } from "../../controller";
import { playBack, playMove, playSelect } from "../../sound";

type Kind = "monitor" | "audio" | "bluetooth" | "volume";

type Action = {
  key: Kind;
  icon: string;
  label: string;
  desc: string;
  /** When true the tile is shown disabled (stub for upcoming feature). */
  disabled?: boolean;
};

export function DevicesMenu({
  onClose,
  onPickMonitor,
  onPickAudio,
  onPickBluetooth,
  onPickVolume,
}: {
  onClose: () => void;
  onPickMonitor: () => void;
  onPickAudio: () => void;
  onPickBluetooth: () => void;
  onPickVolume: () => void;
}) {
  const actions: Action[] = [
    {
      key: "monitor", icon: "🖥", label: "Монитор",
      desc: "Снова покажет выбор активного монитора на всех экранах — то же, что было на 🖥 в v0.9.",
    },
    {
      key: "audio", icon: "🎧", label: "Аудио",
      desc: "Назначить вывод / ввод по умолчанию. Меняет дефолт Windows для роли Console + Multimedia.",
    },
    {
      key: "volume", icon: "🔊", label: "Громкость",
      desc: "Системная громкость дефолтного устройства. ← → шаг, LB/RB крупный шаг, A — Mute.",
    },
    {
      key: "bluetooth", icon: "📶", label: "Bluetooth",
      desc: "Подключить парные BT-наушники. Пайринг — через Параметры Windows; здесь только connect/disconnect.",
    },
  ];

  const [active, setActive] = useState(0);
  const lastMove = useRef(0);

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

  // Same stale-closure guard as PowerMenu — pass the index explicitly.
  function activate(i: number) {
    const a = actions[i];
    if (a.disabled) return;
    playSelect();
    onClose();
    if (a.key === "monitor") onPickMonitor();
    else if (a.key === "audio") onPickAudio();
    else if (a.key === "bluetooth") onPickBluetooth();
    else if (a.key === "volume") onPickVolume();
  }

  useControllerNav((dir) => {
    if (dir === "left")  move(-1);
    if (dir === "right") move(+1);
  });
  useControllerButton((btn) => {
    if (btn === "a") activate(active);
    else if (btn === "b" || btn === "back") { playBack(); onClose(); }
  });

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "ArrowLeft") { e.preventDefault(); move(-1); }
      else if (e.key === "ArrowRight") { e.preventDefault(); move(+1); }
      else if (e.key === "Enter") { e.preventDefault(); activate(active); }
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
      <h1 className="mb-10 text-3xl font-semibold text-gray-100">Устройства</h1>
      <div
        className="grid w-full max-w-5xl grid-cols-2 gap-6 sm:grid-cols-4"
        onClick={(e) => e.stopPropagation()}
      >
        {actions.map((a, i) => {
          const isActive = i === active;
          return (
            <button
              key={a.key}
              onMouseEnter={() => setActive(i)}
              onClick={() => { setActive(i); activate(i); }}
              className={clsx(
                "flex flex-col items-center gap-3 rounded-2xl border-2 p-8 text-center transition-all",
                a.disabled && "opacity-50",
                isActive && !a.disabled
                  ? "border-accent bg-accent/20 shadow-[0_10px_40px_rgba(124,92,255,0.45)] scale-105"
                  : isActive && a.disabled
                  ? "border-white/30 bg-white/10 scale-105"
                  : "border-white/10 bg-white/5 hover:bg-white/10",
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
