// ShellBackupsView — full-screen translucent-tiles backup browser used
// in shell mode. Replaces the v0.10.0/v0.10.1 centered <Modal> wrapper,
// which read as a different family from the PowerMenu / DevicesMenu /
// BluetoothPicker overlays (all of which are full-screen, blurred-back,
// no card).
//
// Controller bindings mirror the other shell pickers:
//   ↑ ↓ / LS up-down       step through rows
//   RS                     fine scroll
//   A                      "show archive in folder" for the focused row
//                          (best-guess action; non-destructive)
//   B / Esc                close
//
// We render our own row layout here rather than reusing <BackupsPage>
// table — the table cells need to be focusable individual rows in
// shell mode, and adding role="button"/tabIndex to a <tr> is awkward.
// Same column data, different layout.

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { api, formatBytes, formatDate, type GameView, type Snapshot } from "../../api";
import { useControllerButton, useControllerNav, useControllerScroll } from "../../controller";
import { playBack, playMove, playSelect } from "../../sound";

type Row = { gameName: string; sn: Snapshot };

export function ShellBackupsView({
  games,
  onClose,
}: {
  games: GameView[];
  onClose: () => void;
}) {
  const flat = useMemo<Row[]>(() => {
    const rows: Row[] = [];
    for (const g of games) for (const sn of g.snapshots) rows.push({ gameName: g.game.name, sn });
    rows.sort((a, b) => b.sn.createdAt - a.sn.createdAt);
    return rows;
  }, [games]);

  const [idx, setIdx] = useState(0);
  const lastMove = useRef(0);
  const listRef = useRef<HTMLDivElement | null>(null);
  const rowRefs = useRef<(HTMLButtonElement | null)[]>([]);

  useEffect(() => {
    if (idx >= flat.length) setIdx(Math.max(0, flat.length - 1));
  }, [flat.length, idx]);

  const focusRow = useCallback((i: number) => {
    document.querySelectorAll<HTMLElement>('[data-gs-focused="true"]')
      .forEach((el) => el.removeAttribute("data-gs-focused"));
    const el = rowRefs.current[i];
    if (!el) return;
    el.setAttribute("data-gs-focused", "true");
    el.focus({ preventScroll: true });
    el.scrollIntoView({ block: "nearest", behavior: "smooth" });
  }, []);

  function step(delta: number) {
    if (!flat.length) return;
    const now = Date.now();
    if (now - lastMove.current < 100) return;
    lastMove.current = now;
    setIdx((i) => {
      const next = Math.max(0, Math.min(flat.length - 1, i + delta));
      if (next !== i) playMove();
      return next;
    });
  }

  // Sync the focus marker whenever idx changes (from any source: gamepad,
  // keyboard, mouse hover).
  useEffect(() => { focusRow(idx); }, [idx, focusRow]);

  function activate() {
    const row = flat[idx]; if (!row) return;
    playSelect();
    api.ShowItemInFolder(row.sn.archivePath).catch((e) =>
      api.Toast("error", "Открыть папку: " + String(e)));
  }

  useControllerNav((dir) => {
    if (dir === "up" || dir === "left")   step(-1);
    if (dir === "down" || dir === "right") step(+1);
  });
  useControllerScroll(({ dy }) => {
    listRef.current?.scrollBy({ top: dy * 18, behavior: "auto" });
  });
  useControllerButton((btn) => {
    if (btn === "a") activate();
    else if (btn === "b" || btn === "back") { playBack(); onClose(); }
  });

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "ArrowUp")       { e.preventDefault(); step(-1); }
      else if (e.key === "ArrowDown") { e.preventDefault(); step(+1); }
      else if (e.key === "PageUp")    { e.preventDefault();
        listRef.current?.scrollBy({ top: -360, behavior: "smooth" });
      } else if (e.key === "PageDown") { e.preventDefault();
        listRef.current?.scrollBy({ top: +360, behavior: "smooth" });
      } else if (e.key === "Enter") { e.preventDefault(); activate(); }
      else if (e.key === "Escape") { e.preventDefault(); playBack(); onClose(); }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [idx, flat]);

  return (
    <div
      className="fixed inset-0 z-[1500] flex flex-col items-center bg-black/85 p-8 backdrop-blur-md"
      onClick={onClose}
    >
      <div
        className="flex w-full max-w-6xl flex-1 flex-col gap-4 overflow-hidden"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center justify-between">
          <h1 className="text-3xl font-semibold text-gray-100">Бэкапы</h1>
          <div className="text-xs text-muted">
            ↑ ↓ выбор · RS прокрутка · A — открыть архив в папке · B / Esc закрыть
          </div>
        </div>

        {flat.length === 0 ? (
          <div className="rounded-2xl border-2 border-white/10 bg-white/5 p-8 text-center text-sm text-muted">
            Снэпшотов пока нет. Сделай один из «Подробнее → Бэкап» на главном.
          </div>
        ) : (
          <div
            ref={listRef}
            className="flex-1 overflow-y-auto rounded-2xl border border-white/10 bg-white/5 p-3"
          >
            {/* Column header — labels only, real rows below carry the data. */}
            <div className="mb-2 grid grid-cols-[2fr_1.4fr_0.8fr_0.6fr_0.7fr_3fr] gap-3 px-3 py-1 text-[11px] uppercase tracking-wide text-muted">
              <div>Game</div>
              <div>When</div>
              <div>Trigger</div>
              <div className="text-right">Files</div>
              <div className="text-right">Size</div>
              <div>Archive</div>
            </div>

            <div className="grid gap-1">
              {flat.map(({ gameName, sn }, i) => (
                <button
                  key={sn.id}
                  ref={(el) => { rowRefs.current[i] = el; }}
                  onMouseEnter={() => setIdx(i)}
                  onClick={(e) => { e.stopPropagation(); setIdx(i); activate(); }}
                  className={
                    "grid grid-cols-[2fr_1.4fr_0.8fr_0.6fr_0.7fr_3fr] gap-3 rounded-xl border-2 px-3 py-2 text-left text-sm transition " +
                    (i === idx
                      ? "border-accent bg-accent/20 shadow-[0_8px_25px_rgba(124,92,255,0.35)]"
                      : "border-white/10 bg-white/5 hover:bg-white/10")
                  }
                >
                  <div className="truncate font-medium text-gray-100" title={gameName}>{gameName}</div>
                  <div className="truncate text-gray-300">{formatDate(sn.createdAt)}</div>
                  <div className="truncate text-gray-300">{sn.trigger}</div>
                  <div className="text-right text-gray-300">{sn.fileCount}</div>
                  <div className="text-right text-gray-300">{formatBytes(sn.compressedBytes)}</div>
                  <div className="truncate text-xs text-muted" title={sn.archivePath}>{sn.archivePath}</div>
                </button>
              ))}
            </div>
          </div>
        )}

        <div className="flex justify-end">
          <button className="btn" onClick={() => { playBack(); onClose(); }}>
            Закрыть (B / Esc)
          </button>
        </div>
      </div>
    </div>
  );
}
