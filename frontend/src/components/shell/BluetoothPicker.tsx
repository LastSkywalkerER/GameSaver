// BluetoothPicker — full-screen modal listing paired Bluetooth devices
// and letting the user toggle Connect / Disconnect from a controller.
//
// Pairing is intentionally NOT in here — Windows pairing wants a PIN
// dialog and a sustained inquiry phase that takes minutes to do right.
// For paired devices a single ENABLE call on the audio service GUIDs
// is "connect"; same with DISABLE for "disconnect". The button at the
// bottom punts to the system "Add Bluetooth" applet for unpaired
// devices.
//
// Controller bindings (mirror AudioPicker):
//   ↑↓ / LS up-down       move within list
//   A / Enter             connect/disconnect the focused device
//   B / Esc               close
//   X / R                 refresh list
//
// Connect/Disconnect calls BLOCK on the backend (up to ~30 s while
// the radio negotiates). We mark `busy` for the in-flight ID so the UI
// shows a spinner; re-presses on the same row are no-ops while busy.

import { useEffect, useRef, useState } from "react";
import clsx from "clsx";
import { api } from "../../api";
import { useControllerButton, useControllerNav } from "../../controller";
import { playBack, playMove, playSelect } from "../../sound";

export type BTDevice = {
  id: string;       // 12-hex MAC (no separators)
  address: string;  // "AA:BB:CC:DD:EE:FF"
  name: string;
  connected: boolean;
  isAudio: boolean;
  classHex: string;
};

export function BluetoothPicker({ onDone }: { onDone: () => void }) {
  const [devices, setDevices] = useState<BTDevice[]>([]);
  const [loading, setLoading] = useState(true);
  const [busyId, setBusyId] = useState<string | null>(null);
  const [idx, setIdx] = useState(0);
  const lastMove = useRef(0);

  async function refresh() {
    setLoading(true);
    try {
      const list: any = await (api as any).ListBluetoothDevices();
      const arr: BTDevice[] = Array.isArray(list) ? list : [];
      // Sort: audio devices first (most common pick), then by name.
      arr.sort((a, b) => {
        if (a.isAudio !== b.isAudio) return a.isAudio ? -1 : 1;
        return a.name.localeCompare(b.name);
      });
      setDevices(arr);
    } catch (e) {
      api.Toast("error", "Список Bluetooth: " + String(e));
    } finally {
      setLoading(false);
    }
  }
  useEffect(() => { refresh(); }, []);

  useEffect(() => { if (idx >= devices.length) setIdx(Math.max(0, devices.length - 1)); }, [devices.length, idx]);

  function step(delta: number) {
    const now = Date.now();
    if (now - lastMove.current < 100) return;
    lastMove.current = now;
    setIdx((i) => {
      const next = Math.max(0, Math.min(devices.length - 1, i + delta));
      if (next !== i) playMove();
      return next;
    });
  }

  async function toggle() {
    const dev = devices[idx];
    if (!dev || busyId) return;
    playSelect();
    setBusyId(dev.id);
    try {
      if (dev.connected) {
        await (api as any).DisconnectBluetoothDevice(dev.id);
        api.Toast("success", `Отключено: ${dev.name}`);
      } else {
        await (api as any).ConnectBluetoothDevice(dev.id);
        api.Toast("success", `Подключено: ${dev.name}`);
      }
      await refresh();
    } catch (e) {
      api.Toast("error", "BT: " + String(e));
    } finally { setBusyId(null); }
  }

  useControllerNav((dir) => {
    if (dir === "up")   step(-1);
    if (dir === "down") step(+1);
  });
  useControllerButton((btn) => {
    if (btn === "a") toggle();
    else if (btn === "x" || btn === "rb") refresh();
    else if (btn === "b" || btn === "back") { playBack(); onDone(); }
  });

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "ArrowUp")   { e.preventDefault(); step(-1); }
      else if (e.key === "ArrowDown") { e.preventDefault(); step(+1); }
      else if (e.key === "Enter") { e.preventDefault(); toggle(); }
      else if (e.key === "Escape") { e.preventDefault(); playBack(); onDone(); }
      else if (e.key.toLowerCase() === "r") { e.preventDefault(); refresh(); }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [idx, devices, busyId]);

  return (
    <div className="fixed inset-0 z-[2000] flex flex-col items-center justify-center bg-black/95 p-8 backdrop-blur-md">
      <h1 className="mb-2 text-3xl font-bold">Bluetooth — устройства</h1>
      <p className="mb-6 max-w-2xl text-center text-sm text-gray-400">
        Парные устройства. ↑↓ — выбрать, A / Enter — подключить или
        отключить, X / R — обновить список, B / Esc — закрыть.
        Подключение блокирует приложение пока радио договаривается с
        наушниками (до ~30с).
      </p>

      <div className="grid w-full max-w-3xl gap-2">
        {devices.length === 0 && !loading && (
          <div className="rounded-2xl border-2 border-white/10 bg-white/5 p-6 text-center text-sm text-muted">
            Нет парных устройств. Сначала запарь наушники через
            «Параметры Windows → Bluetooth».
          </div>
        )}
        {devices.map((d, i) => {
          const isFocus = i === idx;
          const busy = busyId === d.id;
          return (
            <button
              key={d.id}
              onClick={(e) => { e.stopPropagation(); setIdx(i); toggle(); }}
              onMouseEnter={() => setIdx(i)}
              className={clsx(
                "flex items-center justify-between gap-3 rounded-xl border-2 px-4 py-3 text-left transition",
                isFocus
                  ? "border-accent bg-accent/25 scale-[1.01] shadow-[0_10px_30px_rgba(124,92,255,0.4)]"
                  : "border-white/10 bg-white/5 hover:bg-white/10",
                busy && "opacity-70 cursor-wait",
              )}
            >
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-2">
                  <span className="text-lg">{d.isAudio ? "🎧" : "📶"}</span>
                  <span className="truncate text-base text-gray-100">{d.name}</span>
                </div>
                <div className="mt-0.5 text-xs text-muted">
                  {d.address} · {d.classHex}
                </div>
              </div>
              <div className="shrink-0">
                {busy ? (
                  <span className="rounded bg-amber-700/40 px-3 py-1 text-xs text-amber-200">
                    ↻ работаю…
                  </span>
                ) : d.connected ? (
                  <span className="rounded bg-emerald-700/40 px-3 py-1 text-xs text-emerald-200">
                    подключено
                  </span>
                ) : (
                  <span className="rounded bg-white/10 px-3 py-1 text-xs text-gray-300">
                    подключить
                  </span>
                )}
              </div>
            </button>
          );
        })}
      </div>

      {loading && <div className="mt-6 text-sm text-muted">Загружаю…</div>}

      <div className="mt-8 flex flex-wrap gap-3">
        <button
          className="btn"
          onClick={refresh}
        >
          ↻ Обновить (X / R)
        </button>
        <button
          className="btn"
          onClick={async () => {
            try { await (api as any).OpenWindowsBluetoothSettings(); }
            catch (e) { api.Toast("error", "Не открылось: " + String(e)); }
          }}
          title="Откроет системный мастер «Добавить устройство» для пайринга новых наушников."
        >
          ➕ Запарить новое (Windows)
        </button>
        <button
          className="btn"
          onClick={() => { playBack(); onDone(); }}
        >
          Закрыть (B / Esc)
        </button>
      </div>
    </div>
  );
}
