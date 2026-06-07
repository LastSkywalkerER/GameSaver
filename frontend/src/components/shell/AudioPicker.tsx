// AudioPicker — full-screen modal with two columns (output / input) listing
// active Windows audio endpoints. The current default is highlighted; A on
// the controller / Enter on the keyboard sets the focused device as the new
// default. LB/RB and ← / → switch columns; ↑/↓ moves within a column.
//
// Used both standalone (from the Settings page and the shell corner icon)
// and chained after the monitor picker on shell startup, so a user with a
// controller can pick "monitor + audio device" in a single short flow.

import { useEffect, useMemo, useRef, useState } from "react";
import clsx from "clsx";
import { api } from "../../api";
import { useControllerButton, useControllerNav } from "../../controller";
import { playBack, playMove, playSelect } from "../../sound";

export type AudioDevice = {
  id: string;
  name: string;
  dataFlow: "render" | "capture";
  isDefault: boolean;
};

type Flow = "render" | "capture";

export function AudioPicker({ onDone }: { onDone: () => void }) {
  const [devices, setDevices] = useState<AudioDevice[]>([]);
  const [loading, setLoading] = useState(true);
  const [flow, setFlow] = useState<Flow>("render");
  const [idx, setIdx] = useState(0);
  const [applying, setApplying] = useState(false);
  const lastMove = useRef(0);

  async function refresh() {
    try {
      const list: any = await (api as any).ListAudioDevices();
      setDevices(Array.isArray(list) ? (list as AudioDevice[]) : []);
    } catch (e) {
      api.Toast("error", "Список аудио-устройств: " + String(e));
    } finally {
      setLoading(false);
    }
  }
  useEffect(() => { refresh(); }, []);

  const col = useMemo(() => devices.filter((d) => d.dataFlow === flow), [devices, flow]);

  // Clamp focused index when the visible column shrinks.
  useEffect(() => { if (idx >= col.length) setIdx(Math.max(0, col.length - 1)); }, [col.length, idx]);

  // Pre-select the current default in this column so a single A confirms
  // (matches the monitor picker's "remembered choice = pre-selected" feel).
  useEffect(() => {
    const di = col.findIndex((d) => d.isDefault);
    if (di >= 0) setIdx(di);
  }, [flow, devices.length]);

  function step(delta: number) {
    const now = Date.now();
    if (now - lastMove.current < 100) return;
    lastMove.current = now;
    setIdx((i) => {
      const next = Math.max(0, Math.min(col.length - 1, i + delta));
      if (next !== i) playMove();
      return next;
    });
  }
  function switchFlow(to: Flow) {
    if (to === flow) return;
    playMove();
    setFlow(to);
  }

  // applyDevice switches to a SPECIFIC device. The controller/keyboard
  // path passes col[idx]; a mouse click passes the clicked row's device
  // directly. We must NOT read the focused idx in here: a click does
  // setIdx(i) (async, next render) and then applies immediately, so
  // reading idx would see the STALE pre-selected default (idx is seeded
  // to the current default in the effect above) — which is .isDefault, so
  // we'd just playBack()+onDone() and close WITHOUT switching. That was the
  // bug. Same stale-closure class as PowerMenu v0.7.7 (see frontend-ui.md).
  async function applyDevice(dev: AudioDevice | undefined) {
    if (applying || !dev) return;
    if (dev.isDefault) { playBack(); onDone(); return; }
    playSelect();
    setApplying(true);
    try {
      await (api as any).SetDefaultAudioDevice(dev.id);
      api.Toast("success", `${dev.dataFlow === "render" ? "Вывод" : "Ввод"}: ${dev.name}`);
      await refresh();
    } catch (e) {
      api.Toast("error", "Сменить устройство: " + String(e));
    } finally { setApplying(false); }
  }
  // Controller/keyboard: act on the focused row in the active column.
  function applyChoice() { void applyDevice(col[idx]); }
  // Mouse: resolve the clicked device from its OWN column list (not the
  // focused idx), move the cursor/column there for visual feedback, then
  // switch to it. This is what makes clicking any row — including one in
  // the currently-inactive column — actually switch to that device.
  function pickFromColumn(d: AudioDevice) {
    setFlow(d.dataFlow);
    const list = devices.filter((x) => x.dataFlow === d.dataFlow);
    const i = list.findIndex((x) => x.id === d.id);
    if (i >= 0) setIdx(i);
    void applyDevice(d);
  }

  useControllerNav((dir) => {
    if (dir === "up") step(-1);
    else if (dir === "down") step(+1);
    else if (dir === "left") switchFlow("render");
    else if (dir === "right") switchFlow("capture");
  });
  useControllerButton((btn) => {
    if (btn === "a") applyChoice();
    else if (btn === "lb") switchFlow("render");
    else if (btn === "rb") switchFlow("capture");
    else if (btn === "b" || btn === "back") { playBack(); onDone(); }
  });

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "ArrowUp") { e.preventDefault(); step(-1); }
      else if (e.key === "ArrowDown") { e.preventDefault(); step(+1); }
      else if (e.key === "ArrowLeft") { e.preventDefault(); switchFlow("render"); }
      else if (e.key === "ArrowRight") { e.preventDefault(); switchFlow("capture"); }
      else if (e.key === "Enter") { e.preventDefault(); applyChoice(); }
      else if (e.key === "Escape") { e.preventDefault(); playBack(); onDone(); }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [idx, col, applying]);

  return (
    <div className="fixed inset-0 z-[2000] flex flex-col items-center justify-center bg-black/95 p-8 backdrop-blur-md">
      <h1 className="mb-2 text-3xl font-bold">Аудио — устройство по умолчанию</h1>
      <p className="mb-6 max-w-2xl text-center text-sm text-gray-400">
        Влево/вправо — переключить колонку (вывод/ввод). Вверх/вниз — выбрать. A / Enter — назначить дефолтом. B / Esc — закрыть.
      </p>

      <div className="grid w-full max-w-4xl grid-cols-2 gap-6">
        <Column
          title="🔊 Вывод (output)"
          devices={col.length && flow === "render" ? col : devices.filter((d) => d.dataFlow === "render")}
          active={flow === "render"}
          focusIdx={flow === "render" ? idx : -1}
          onPickColumn={() => switchFlow("render")}
          onPickItem={pickFromColumn}
        />
        <Column
          title="🎤 Ввод (input)"
          devices={col.length && flow === "capture" ? col : devices.filter((d) => d.dataFlow === "capture")}
          active={flow === "capture"}
          focusIdx={flow === "capture" ? idx : -1}
          onPickColumn={() => switchFlow("capture")}
          onPickItem={pickFromColumn}
        />
      </div>

      {loading && <div className="mt-6 text-sm text-muted">Загружаю список…</div>}
      {applying && <div className="mt-6 text-sm text-accent">Переключаем…</div>}

      <div className="mt-8 flex gap-3">
        <button className="btn" onClick={() => { playBack(); onDone(); }}>Закрыть (B / Esc)</button>
      </div>
    </div>
  );
}

function Column({
  title, devices, active, focusIdx, onPickColumn, onPickItem,
}: {
  title: string;
  devices: AudioDevice[];
  active: boolean;
  focusIdx: number;
  onPickColumn: () => void;
  onPickItem: (d: AudioDevice) => void;
}) {
  return (
    <div
      className={clsx(
        "rounded-2xl border-2 p-4 transition-all",
        active ? "border-accent bg-accent/10 shadow-[0_10px_40px_rgba(124,92,255,0.4)]" : "border-white/10 bg-white/5",
      )}
      onClick={onPickColumn}
    >
      <div className="mb-3 text-lg font-semibold text-gray-100">{title}</div>
      {devices.length === 0 && <div className="text-sm text-muted">Нет активных устройств</div>}
      <div className="grid gap-2">
        {devices.map((d, i) => {
          const isFocus = i === focusIdx;
          return (
            <button
              key={d.id}
              onClick={(e) => { e.stopPropagation(); onPickItem(d); }}
              className={clsx(
                "flex items-center justify-between gap-3 rounded-xl border px-3 py-2 text-left transition",
                isFocus
                  ? "border-accent bg-accent/30 scale-[1.02]"
                  : "border-white/10 bg-white/5 hover:bg-white/10",
              )}
            >
              <span className="min-w-0 flex-1 truncate text-sm text-gray-100" title={d.name}>{d.name}</span>
              {d.isDefault && (
                <span className="shrink-0 rounded bg-emerald-700/40 px-2 py-0.5 text-[10px] text-emerald-200">по умолчанию</span>
              )}
            </button>
          );
        })}
      </div>
    </div>
  );
}
