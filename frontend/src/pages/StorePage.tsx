import { useEffect, useMemo, useState } from "react";
import { api, EventsOn, type LibraryCard, type StoreAccount, type StoreClientStatus } from "../api";
import { LibraryCardTile } from "../components/LibraryCardTile";
import { StoreConnectBar } from "../components/StoreConnectBar";
import { getRatings, setRating } from "../ratings";

// StorePage — desktop "Магазины" tab: a slim header (search + store/account
// controls) over a full-bleed merged library grid. Connect/manage lives in the
// shared StoreConnectBar (used in shell mode too).
export function StorePage() {
  const [accounts, setAccounts] = useState<StoreAccount[]>([]);
  const [library, setLibrary] = useState<LibraryCard[]>([]);
  const [clients, setClients] = useState<StoreClientStatus[]>([]);
  const [query, setQuery] = useState("");
  const [ratings, setRatings] = useState<Record<string, number>>(getRatings());

  async function reloadAccounts() {
    try { setAccounts(((await (api as any).ListStoreAccounts()) ?? []) as StoreAccount[]); } catch {}
  }
  async function reloadLibrary() {
    try { setLibrary(((await (api as any).GetAggregatedLibrary()) ?? []) as LibraryCard[]); } catch {}
  }

  useEffect(() => {
    reloadAccounts();
    reloadLibrary();
    (api as any).GetStoreClients?.().then((c: any) => setClients(c || [])).catch(() => {});
    const offs = [
      EventsOn("stores:library-changed", () => { reloadAccounts(); reloadLibrary(); }),
      EventsOn("stores:account-done", () => reloadAccounts()),
      EventsOn("stores:sync-done", () => { reloadAccounts(); reloadLibrary(); }),
    ];
    return () => offs.forEach((o) => { try { (o as any)?.(); } catch {} });
  }, []);

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    const list = q ? library.filter((c) => c.game.name.toLowerCase().includes(q)) : library;
    return [...list].sort((a, b) => {
      const ra = ratings[a.game.id] || 0;
      const rb = ratings[b.game.id] || 0;
      if (ra !== rb) return rb - ra;
      if (a.anyInstalled !== b.anyInstalled) return a.anyInstalled ? -1 : 1;
      return a.game.name.localeCompare(b.game.name);
    });
  }, [library, query, ratings]);

  return (
    <div className="flex h-full flex-col p-4">
      <div className="mb-3 flex flex-wrap items-center gap-2">
        <input
          className="input w-56"
          value={query}
          placeholder="Поиск по библиотеке…"
          onChange={(e) => setQuery(e.target.value)}
        />
        <span className="text-xs text-muted">{filtered.length} игр</span>
        <div className="ml-auto flex flex-wrap items-center gap-2">
          <StoreConnectBar accounts={accounts} clients={clients} onChanged={() => { reloadAccounts(); reloadLibrary(); }} />
          <button className="btn" onClick={() => { (api as any).SyncStoreLibraries?.(); api.Toast("info", "Обновляю библиотеки…"); }}>
            ↻ Всё
          </button>
        </div>
      </div>

      <div className="min-h-0 flex-1 overflow-y-auto">
        {filtered.length === 0 ? (
          <div className="rounded-xl2 border border-border bg-panel p-8 text-center text-sm text-muted">
            Пока пусто. Нажми на иконку магазина вверху справа, подключи аккаунт — и библиотека появится здесь.
          </div>
        ) : (
          <div className="grid grid-cols-3 gap-4 sm:grid-cols-4 md:grid-cols-5 lg:grid-cols-6 xl:grid-cols-7 2xl:grid-cols-8">
            {filtered.map((c) => (
              <LibraryCardTile
                key={c.game.id}
                card={c}
                rating={ratings[c.game.id] || 0}
                onRate={(s) => setRatings(setRating(c.game.id, s))}
              />
            ))}
          </div>
        )}
      </div>
    </div>
  );
}
