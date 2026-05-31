// ShellUpdateModal — PS-style fullscreen update prompt for shell mode.
//
// The desktop UpdateBanner is a thin row at the top of the sidebar/topbar
// layout. Shell mode has no sidebar / topbar (the whole UI is replaced by
// ShellApp), so that banner was never rendered — users on a controller in
// the kiosk shell never saw incoming updates and had no way to install
// them without dropping back to Explorer.
//
// This modal mirrors PowerMenu's shape so the controller bindings are
// familiar:
//   ←/→            move between buttons
//   A / Enter      run the focused action
//   B / Esc        dismiss (== "Позже")
// The "Обновить" button pre-selected on open so a single A confirms.
// Default-dismiss (B/Esc) maps to "Позже" rather than "Пропустить" so a
// fat-finger doesn't silently hide the update for good.

import { useEffect, useRef, useState } from "react";
import clsx from "clsx";
import { api, type UpdateInfo } from "../../api";
import { useControllerButton, useControllerNav } from "../../controller";
import { playBack, playMove, playSelect } from "../../sound";

type Phase = "idle" | "downloading" | "applied" | "error";

type Action = {
  key: string;
  label: string;
  icon: string;
  desc: string;
  variant?: "primary" | "default" | "danger";
  run: () => void | Promise<void>;
};

export function ShellUpdateModal({
  info,
  onDismiss,
}: {
  info: UpdateInfo;
  onDismiss: () => void;
}) {
  const [active, setActive] = useState(0); // "Обновить" pre-selected
  const [phase, setPhase] = useState<Phase>("idle");
  const [err, setErr] = useState<string | null>(null);
  const lastMove = useRef(0);

  const mb = info.assetSize ? (info.assetSize / 1024 / 1024).toFixed(1) : "?";
  const busy = phase === "downloading";

  async function doApply() {
    setPhase("downloading");
    setErr(null);
    try {
      await api.ApplyUpdate();
      setPhase("applied");
      api.Toast("success", `Установлена ${info.latestVersion}. Перезапуск через 2 сек…`);
      // Same restart cadence as the desktop banner — give Wails a beat
      // to flush the toast, then hand control to RestartApp's detached
      // cmd /c ping+start trick.
      setTimeout(() => api.RestartApp().catch(() => {}), 2000);
    } catch (e) {
      setPhase("error");
      setErr(String(e));
      api.Toast("error", "Не удалось обновить: " + String(e));
    }
  }

  async function doSkip() {
    try { await api.SkipUpdate(info.latestVersion); }
    finally { onDismiss(); }
  }

  const actions: Action[] = [
    {
      key: "apply", icon: "⬇", label: busy ? "Качаю…" : phase === "applied" ? "✓ Готово" : "Обновить",
      desc: `Скачать ${info.latestVersion} (~${mb} MB), применить, перезапуститься.`,
      variant: "primary",
      run: () => { if (!busy && phase !== "applied") void doApply(); },
    },
    {
      key: "later", icon: "⏱", label: "Позже",
      desc: "Закрыть окно. Напомню при следующем запуске.",
      run: () => { onDismiss(); },
    },
    {
      key: "skip", icon: "🚫", label: "Пропустить",
      desc: `Больше не показывать ${info.latestVersion}. Следующая версия снова всплывёт.`,
      variant: "danger",
      run: () => { void doSkip(); },
    },
  ];

  function move(delta: number) {
    if (busy) return; // can't navigate while downloading
    const now = Date.now();
    if (now - lastMove.current < 100) return;
    lastMove.current = now;
    setActive((i) => {
      const next = Math.max(0, Math.min(actions.length - 1, i + delta));
      if (next !== i) playMove();
      return next;
    });
  }

  // Same stale-closure guard as PowerMenu: activate takes the index
  // explicitly so a click fires the cursor-target, not the highlight-
  // target. See decisions/0007 / PowerMenu v0.7.7 incident.
  function activate(i: number) {
    if (busy) return;
    playSelect();
    void actions[i].run();
  }

  useControllerNav((dir) => {
    if (dir === "left")  move(-1);
    if (dir === "right") move(+1);
  });
  useControllerButton((btn) => {
    if (btn === "a") activate(active);
    else if (btn === "b" || btn === "back") {
      if (busy) return; // don't let B abort a download — the binary swap is mid-flight
      playBack();
      onDismiss();
    }
  });

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "ArrowLeft")  { e.preventDefault(); move(-1); }
      else if (e.key === "ArrowRight") { e.preventDefault(); move(+1); }
      else if (e.key === "Enter") { e.preventDefault(); activate(active); }
      else if (e.key === "Escape") {
        if (busy) return;
        e.preventDefault(); playBack(); onDismiss();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [active, busy]);

  // Trim/normalize release notes — the GitHub auto-generated body is
  // typically a long markdown blob; we want a quick summary, not a wall
  // of bullets the user has to scroll. 480 chars ≈ first paragraph.
  const notes = (info.releaseNotes ?? "").trim();
  const shortNotes = notes.length > 480 ? notes.slice(0, 480) + "…" : notes;

  return (
    <div
      className="fixed inset-0 z-[1700] flex flex-col items-center justify-center bg-black/85 p-8 backdrop-blur-md"
      onClick={() => { if (!busy) onDismiss(); }}
    >
      <div
        className="flex w-full max-w-5xl flex-col items-center gap-6"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-baseline gap-3">
          <span className="text-2xl">⬆</span>
          <h1 className="text-3xl font-semibold text-gray-100">Доступно обновление</h1>
        </div>
        <div className="text-center text-lg text-gray-300">
          <span className="rounded-md bg-accent/25 px-2 py-1 font-semibold text-accent">
            {info.latestVersion}
          </span>
          <span className="ml-3 text-sm text-muted">текущая {info.currentVersion} · ~{mb} MB</span>
        </div>

        {shortNotes && (
          <div className="w-full max-w-3xl rounded-2xl border border-white/10 bg-white/5 p-4">
            <div className="mb-2 text-xs uppercase tracking-wide text-muted">Что нового</div>
            <div className="max-h-40 overflow-y-auto whitespace-pre-line text-sm leading-relaxed text-gray-200">
              {shortNotes}
            </div>
          </div>
        )}

        {err && (
          <div className="w-full max-w-3xl rounded-xl border border-red-500/40 bg-red-900/30 p-3 text-sm text-red-200">
            {err}
          </div>
        )}

        <div className="grid w-full grid-cols-1 gap-6 md:grid-cols-3">
          {actions.map((a, i) => {
            const isActive = i === active;
            const primary = a.variant === "primary";
            const danger  = a.variant === "danger";
            return (
              <button
                key={a.key}
                disabled={busy}
                onMouseEnter={() => !busy && setActive(i)}
                onClick={() => { if (!busy) { setActive(i); activate(i); } }}
                className={clsx(
                  "flex flex-col items-center gap-3 rounded-2xl border-2 p-8 text-center transition-all",
                  busy && "opacity-50 cursor-wait",
                  isActive && primary
                    ? "border-accent bg-accent/25 shadow-[0_10px_40px_rgba(124,92,255,0.5)] scale-105"
                    : isActive && danger
                    ? "border-red-500 bg-red-900/30 shadow-[0_10px_40px_rgba(239,68,68,0.45)] scale-105"
                    : isActive
                    ? "border-white/40 bg-white/15 scale-105"
                    : "border-white/10 bg-white/5 hover:bg-white/10"
                )}
              >
                <div className="text-5xl">{a.icon}</div>
                <div className="text-lg font-semibold text-gray-100">{a.label}</div>
                <div className="text-xs leading-snug text-gray-400">{a.desc}</div>
              </button>
            );
          })}
        </div>

        <div className="text-xs text-muted">
          ← → выбор · A / Enter подтвердить · B / Esc — «Позже»
        </div>
      </div>
    </div>
  );
}
