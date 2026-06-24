// BluetoothPicker — full-screen modal that DISCOVERS, PAIRS and connects
// Bluetooth devices from inside the controller-driven shell, no Windows wizard
// needed for the common case (classic / dual-mode headsets).
//
// On open it pulls the paired snapshot, subscribes to the backend's
// bluetooth:* events, and kicks off an active inquiry. Discovered (unpaired)
// devices stream in via bluetooth:found and merge with the paired list.
//
// Controller bindings:
//   ↑↓ / LS up-down   move within list
//   A / Enter         primary action — Pair (unpaired) · Connect/enable audio (paired)
//   Y / Del           disconnect a connected device
//   X / RB            rescan (new inquiry)
//   LB                fall back to the Windows wizard (for BLE-only devices)
//   B / Esc           close
//
// Connect returns a classified ConnectResult: the expected non-fatal outcomes
// (asleep, no audio profile, not paired yet) show as an inline amber hint, NOT
// a red error toast — fixing the old "press → instant error" behavior.

import { useEffect, useRef, useState } from "react";
import clsx from "clsx";
import { api, EventsOn } from "../../api";
import { useControllerButton, useControllerNav } from "../../controller";
import { playBack, playMove, playSelect } from "../../sound";

export type BTDevice = {
  id: string;       // 12-hex MAC (no separators)
  address: string;  // "AA:BB:CC:DD:EE:FF"
  name: string;
  connected: boolean;  // radio/ACL link up
  paired: boolean;     // bonded with Windows
  remembered: boolean;
  isAudio: boolean;
  classHex: string;
};

type ConnectResult = { ok: boolean; status: string; detail: string };

type Status = "unpaired" | "paired-idle" | "connected";
function statusOf(d: BTDevice): Status {
  if (!d.paired) return "unpaired";
  return d.connected ? "connected" : "paired-idle";
}

// Sort: connected → paired-idle → unpaired; audio-first within each group, then
// by name — so the most-actionable headset is on top.
function sortDevices(arr: BTDevice[]): BTDevice[] {
  const rank = (d: BTDevice) => (d.connected ? 0 : d.paired ? 1 : 2);
  return [...arr].sort((a, b) => {
    if (rank(a) !== rank(b)) return rank(a) - rank(b);
    if (a.isAudio !== b.isAudio) return a.isAudio ? -1 : 1;
    return a.name.localeCompare(b.name);
  });
}

export function BluetoothPicker({ onDone }: { onDone: () => void }) {
  const [byId, setById] = useState<Record<string, BTDevice>>({});
  const [scanning, setScanning] = useState(false);
  const [busyId, setBusyId] = useState<string | null>(null);
  const [hints, setHints] = useState<Record<string, string>>({});
  const [idx, setIdx] = useState(0);
  const lastMove = useRef(0);

  const devices = sortDevices(Object.values(byId));

  function upsert(d: BTDevice) {
    setById((m) => ({ ...m, [d.id]: { ...m[d.id], ...d } }));
  }

  async function refreshPaired() {
    try {
      const list: any = await (api as any).ListBluetoothDevices();
      const arr: BTDevice[] = Array.isArray(list) ? list : [];
      setById((m) => {
        const next = { ...m };
        for (const d of arr) next[d.id] = { ...next[d.id], ...d };
        return next;
      });
    } catch (e) {
      api.Toast("error", "Список Bluetooth: " + String(e));
    }
  }

  function rescan() {
    setHints({});
    try { (api as any).ScanBluetoothDevices(); } catch {}
  }

  // Mount: pull the paired snapshot, subscribe to backend events FIRST, then
  // start an inquiry so unpaired devices appear without a button press. Wails
  // doesn't replay events, hence the explicit getter on mount.
  useEffect(() => {
    refreshPaired();
    const offs = [
      EventsOn("bluetooth:scan-start", () => setScanning(true)),
      EventsOn("bluetooth:found", (d: any) => { if (d) upsert(d as BTDevice); }),
      EventsOn("bluetooth:scan-done", () => setScanning(false)),
      EventsOn("bluetooth:scan-error", (m: any) => { setScanning(false); api.Toast("error", "BT поиск: " + String(m)); }),
      EventsOn("bluetooth:device-changed", () => refreshPaired()),
      EventsOn("bluetooth:pair-start", (p: any) => { if (p?.id) setBusyId(p.id); }),
      EventsOn("bluetooth:pair-done", (p: any) => {
        if (!p) return;
        setBusyId((cur) => (cur === p.id ? null : cur));
        if (p.ok) { playSelect(); api.Toast("success", "Запарено и подключено"); refreshPaired(); }
        else setHints((h) => ({ ...h, [p.id]: p.detail || "не удалось запарить" }));
      }),
    ];
    (api as any).ScanBluetoothDevices?.();
    return () => { offs.forEach((o) => { try { (o as any)?.(); } catch {} }); };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    if (idx >= devices.length) setIdx(Math.max(0, devices.length - 1));
  }, [devices.length, idx]);

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

  function clearHint(id: string) {
    setHints((h) => { const n = { ...h }; delete n[id]; return n; });
  }

  // Primary action — pair an unpaired device, else connect / (re)enable audio.
  // Pass the device explicitly (never read devices[idx] from a stale closure).
  async function act(dev: BTDevice | undefined) {
    if (!dev || busyId) return;
    playSelect();
    clearHint(dev.id);
    if (!dev.paired) {
      setBusyId(dev.id); // cleared on the pair-done event
      try { await (api as any).PairBluetoothDevice(dev.id); }
      catch (e) { api.Toast("error", "BT: " + String(e)); setBusyId(null); }
      return;
    }
    setBusyId(dev.id);
    try {
      const r: ConnectResult = await (api as any).ConnectBluetoothDevice(dev.id);
      if (r?.ok) {
        api.Toast("success", `Подключено: ${dev.name}`);
      } else if (r) {
        // Expected, non-fatal states → inline amber hint; only a real error
        // gets a red toast.
        setHints((h) => ({ ...h, [dev.id]: r.detail || r.status }));
        if (r.status === "error") api.Toast("error", "BT: " + (r.detail || "ошибка"));
      }
      await refreshPaired();
    } catch (e) {
      api.Toast("error", "BT: " + String(e));
    } finally { setBusyId(null); }
  }

  async function disconnect(dev: BTDevice | undefined) {
    if (!dev || busyId || !dev.paired || !dev.connected) return;
    playSelect();
    setBusyId(dev.id);
    try {
      await (api as any).DisconnectBluetoothDevice(dev.id);
      api.Toast("success", `Отключено: ${dev.name}`);
      await refreshPaired();
    } catch (e) { api.Toast("error", "BT: " + String(e)); }
    finally { setBusyId(null); }
  }

  async function windowsFallback() {
    try { await (api as any).OpenWindowsBluetoothSettings(); }
    catch (e) { api.Toast("error", "Не открылось: " + String(e)); }
  }

  useControllerNav((dir) => {
    if (dir === "up") step(-1);
    if (dir === "down") step(+1);
  });
  useControllerButton((btn) => {
    if (btn === "a") act(devices[idx]);
    else if (btn === "y") disconnect(devices[idx]);
    else if (btn === "x" || btn === "rb") rescan();
    else if (btn === "lb") windowsFallback();
    else if (btn === "b" || btn === "back") { playBack(); onDone(); }
  });

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "ArrowUp") { e.preventDefault(); step(-1); }
      else if (e.key === "ArrowDown") { e.preventDefault(); step(+1); }
      else if (e.key === "Enter") { e.preventDefault(); act(devices[idx]); }
      else if (e.key === "Delete" || e.key === "Backspace") { e.preventDefault(); disconnect(devices[idx]); }
      else if (e.key === "Escape") { e.preventDefault(); playBack(); onDone(); }
      else if (e.key.toLowerCase() === "r") { e.preventDefault(); rescan(); }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [idx, devices, busyId]);

  return (
    <div className="fixed inset-0 z-[2000] flex flex-col items-center justify-center bg-black/95 p-8 backdrop-blur-md">
      <h1 className="mb-2 text-3xl font-bold">Bluetooth — поиск и подключение</h1>
      <p className="mb-4 max-w-2xl text-center text-sm text-gray-400">
        ↑↓ — выбрать, A — запарить / подключить, Y — отключить, X — искать
        снова, B — закрыть. Чтобы запарить новые наушники, переведи их в режим
        сопряжения. Подключение может занять до ~30с.
      </p>

      <div className="mb-3 h-5 text-sm">
        {scanning ? (
          <span className="animate-pulse text-accent">🔎 Ищу устройства поблизости…</span>
        ) : (
          <span className="text-muted">Готово. X / R — искать снова.</span>
        )}
      </div>

      <div className="grid w-full max-w-3xl gap-2">
        {devices.length === 0 && !scanning && (
          <div className="rounded-2xl border-2 border-white/10 bg-white/5 p-6 text-center text-sm text-muted">
            Ничего не найдено. Переведи наушники в режим сопряжения и нажми
            «Искать снова» (X / R).
          </div>
        )}
        {devices.map((d, i) => {
          const isFocus = i === idx;
          const busy = busyId === d.id;
          const st = statusOf(d);
          const hint = hints[d.id];
          return (
            <button
              key={d.id}
              onClick={(e) => { e.stopPropagation(); setIdx(i); act(d); }}
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
                {hint && (
                  <div className="mt-1 text-xs text-amber-300">⚠ {hint}</div>
                )}
              </div>
              <div className="flex shrink-0 items-center gap-2">
                {busy ? (
                  <span className="rounded bg-amber-700/40 px-3 py-1 text-xs text-amber-200">↻ работаю…</span>
                ) : st === "connected" ? (
                  <span className="rounded bg-emerald-700/40 px-3 py-1 text-xs text-emerald-200">подключено</span>
                ) : st === "paired-idle" ? (
                  <span className="rounded bg-white/10 px-3 py-1 text-xs text-gray-300">подключить</span>
                ) : (
                  <span className="rounded bg-sky-700/40 px-3 py-1 text-xs text-sky-200">запарить</span>
                )}
                {st === "connected" && !busy && (
                  <span
                    role="button"
                    onClick={(e) => { e.stopPropagation(); setIdx(i); disconnect(d); }}
                    className="rounded bg-white/5 px-2 py-1 text-xs text-gray-400 hover:bg-white/10 hover:text-gray-200"
                    title="Отключить (Y / Del)"
                  >
                    ✕
                  </span>
                )}
              </div>
            </button>
          );
        })}
      </div>

      <div className="mt-8 flex flex-wrap gap-3">
        <button className="btn" onClick={rescan}>🔎 Искать снова (X / R)</button>
        <button
          className="btn"
          onClick={windowsFallback}
          title="Для BLE-наушников, которые не находятся через обычный поиск — открывает мастер Windows."
        >
          ➕ Через Windows (LB)
        </button>
        <button className="btn" onClick={() => { playBack(); onDone(); }}>Закрыть (B / Esc)</button>
      </div>
    </div>
  );
}
