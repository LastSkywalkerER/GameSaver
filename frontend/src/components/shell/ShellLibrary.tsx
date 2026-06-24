// ShellLibrary — full-screen, controller-navigable overlay for the merged store
// library in shell mode. Browse / launch (A) / rate (LB/RB) / re-sync (X) with
// the gamepad; connect & manage store accounts with the mouse via the shared
// StoreConnectBar (browser-redirect logins work in shell too). While a connect
// modal is open the gamepad grid is gated so it doesn't move behind it.

import { useEffect, useMemo, useRef, useState } from "react";
import { api, EventsOn, type LibraryCard, type StoreAccount, type StoreClientStatus } from "../../api";
import { useControllerButton, useControllerNav } from "../../controller";
import { LibraryCardTile } from "../LibraryCardTile";
import { StoreConnectBar } from "../StoreConnectBar";
import { getRatings, setRating } from "../../ratings";
import { playBack, playMove, playSelect } from "../../sound";

const COLS = 6;

function sortLib(library: LibraryCard[], ratings: Record<string, number>): LibraryCard[] {
  return [...library].sort((a, b) => {
    const ra = ratings[a.game.id] || 0;
    const rb = ratings[b.game.id] || 0;
    if (ra !== rb) return rb - ra;
    if (a.anyInstalled !== b.anyInstalled) return a.anyInstalled ? -1 : 1;
    return a.game.name.localeCompare(b.game.name);
  });
}

export function ShellLibrary({
  onClose,
  onLaunch,
}: {
  onClose: () => void;
  onLaunch?: (gameId: string, installationId: string) => void;
}) {
  const [library, setLibrary] = useState<LibraryCard[]>([]);
  const [accounts, setAccounts] = useState<StoreAccount[]>([]);
  const [clients, setClients] = useState<StoreClientStatus[]>([]);
  const [idx, setIdx] = useState(0);
  const [ratings, setRatings] = useState<Record<string, number>>(getRatings());
  const [connectOpen, setConnectOpen] = useState(false);
  const lastMove = useRef(0);

  const sorted = useMemo(() => sortLib(library, ratings), [library, ratings]);

  async function reload() {
    try { setLibrary(((await (api as any).GetAggregatedLibrary()) ?? []) as LibraryCard[]); } catch {}
    try { setAccounts(((await (api as any).ListStoreAccounts()) ?? []) as StoreAccount[]); } catch {}
    try { setClients(((await (api as any).GetStoreClients?.()) ?? []) as StoreClientStatus[]); } catch {}
  }

  useEffect(() => {
    reload();
    const offs = [
      EventsOn("stores:library-changed", () => reload()),
      EventsOn("stores:sync-done", () => reload()),
      EventsOn("stores:account-done", () => reload()),
    ];
    return () => offs.forEach((o) => { try { (o as any)?.(); } catch {} });
  }, []);

  useEffect(() => { if (idx >= sorted.length) setIdx(Math.max(0, sorted.length - 1)); }, [sorted.length, idx]);

  function move(delta: number) {
    const now = Date.now();
    if (now - lastMove.current < 90) return;
    lastMove.current = now;
    setIdx((i) => {
      const next = Math.max(0, Math.min(sorted.length - 1, i + delta));
      if (next !== i) playMove();
      return next;
    });
  }

  function sync() { playSelect(); (api as any).SyncStoreLibraries?.(); api.Toast("info", "Обновляю библиотеки…"); }

  function rate(delta: number) {
    const card = sorted[idx];
    if (!card) return;
    const cur = ratings[card.game.id] || 0;
    const next = Math.max(0, Math.min(5, cur + delta));
    if (next === cur) return;
    playMove();
    const nr = setRating(card.game.id, next);
    setRatings(nr);
    const ni = sortLib(library, nr).findIndex((c) => c.game.id === card.game.id);
    if (ni >= 0) setIdx(ni);
  }

  function launch() {
    const card = sorted[idx];
    if (!card) return;
    if (card.anyInstalled && card.installations[0] && onLaunch) {
      onLaunch(card.game.id, card.installations[0].id);
    } else {
      api.Toast("info", "Игра не установлена — открой её в магазине");
    }
  }

  useControllerNav((dir) => {
    if (connectOpen) return;
    if (dir === "left") move(-1);
    else if (dir === "right") move(+1);
    else if (dir === "up") move(-COLS);
    else if (dir === "down") move(+COLS);
  });
  useControllerButton((btn) => {
    if (connectOpen) return;
    if (btn === "a") launch();
    else if (btn === "x") sync();
    else if (btn === "lb") rate(-1);
    else if (btn === "rb") rate(+1);
    else if (btn === "b" || btn === "back") { playBack(); onClose(); }
  });

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (connectOpen) return; // a connect modal owns the keyboard (typing keys/codes)
      if (e.key === "ArrowLeft") { e.preventDefault(); move(-1); }
      else if (e.key === "ArrowRight") { e.preventDefault(); move(+1); }
      else if (e.key === "ArrowUp") { e.preventDefault(); move(-COLS); }
      else if (e.key === "ArrowDown") { e.preventDefault(); move(+COLS); }
      else if (e.key === "Enter") { e.preventDefault(); launch(); }
      else if (e.key === "+" || e.key === "=") { e.preventDefault(); rate(+1); }
      else if (e.key === "-" || e.key === "_") { e.preventDefault(); rate(-1); }
      else if (e.key === "Escape") { e.preventDefault(); playBack(); onClose(); }
      else if (e.key.toLowerCase() === "r") { e.preventDefault(); sync(); }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [sorted, idx, ratings, library, connectOpen]);

  const summary = useMemo(() => {
    const installed = library.filter((c) => c.anyInstalled).length;
    return `${library.length} игр · ${installed} установлено`;
  }, [library]);

  return (
    <div className="fixed inset-0 z-[1600] flex flex-col bg-black/95 p-8 backdrop-blur-md">
      <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
        <h1 className="text-2xl font-bold">Библиотека магазинов</h1>
        <div className="flex flex-wrap items-center gap-2">
          <StoreConnectBar accounts={accounts} clients={clients} onChanged={reload} onModalChange={setConnectOpen} />
          <button className="btn" onClick={sync}>↻ Всё</button>
        </div>
      </div>
      <p className="mb-4 text-sm text-gray-400">
        ← → ↑ ↓ — выбор · <b>A</b> запустить · <b>LB / RB</b> — оценка ★ · <b>X</b> обновить · <b>B</b> назад · аккаунты — мышкой по иконкам справа. {summary}
      </p>

      {sorted.length === 0 ? (
        <div className="flex flex-1 items-center justify-center text-center text-sm text-muted">
          Пусто. Подключи аккаунты магазинов (иконки справа вверху) или нажми X, чтобы обновить.
        </div>
      ) : (
        <div className="grid flex-1 auto-rows-min grid-cols-6 gap-4 overflow-y-auto pr-1">
          {sorted.map((c, i) => (
            <div key={c.game.id} className={i === idx ? "ring-4 ring-accent rounded-xl2" : ""}>
              <LibraryCardTile
                card={c}
                onClick={() => setIdx(i)}
                rating={ratings[c.game.id] || 0}
                onRate={(s) => setRatings(setRating(c.game.id, s))}
              />
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
