import { useEffect, useState } from "react";
import { api, EventsOn, type GameView, type UpdateInfo } from "./api";
import { Sidebar, type Page } from "./components/Sidebar";
import { TopBar } from "./components/TopBar";
import { Toaster } from "./components/Toaster";
import { GameDrawer } from "./components/GameDrawer";
import { UpdateBanner } from "./components/UpdateBanner";
import { DashboardPage } from "./pages/DashboardPage";
import { LibraryPage } from "./pages/LibraryPage";
import { BackupsPage } from "./pages/BackupsPage";
import { SettingsPage } from "./pages/SettingsPage";
import { setLanguage, useT } from "./i18n";

export default function App() {
  const t = useT();
  const [page, setPage] = useState<Page>("dashboard");
  const [games, setGames] = useState<GameView[]>([]);
  const [scanning, setScanning] = useState(false);
  const [scanned, setScanned] = useState(false);
  const [query, setQuery] = useState("");
  const [filter, setFilter] = useState("all");
  const [opened, setOpened] = useState<GameView | null>(null);
  const [phase, setPhase] = useState<string>("");
  const [update, setUpdate] = useState<UpdateInfo | null>(null);
  // Dashboard sort persists across restarts via localStorage. Falls back to
  // "name" for first-time users / cleared storage.
  const [sortBy, setSortByState] = useState<string>(() => {
    try {
      const v = localStorage.getItem("gs:sortBy") || "name";
      // v0.3.5: renamed "saveSize" → "installSize". Migrate stale values so
      // returning users don't see a phantom-selected dropdown.
      return v === "saveSize" ? "installSize" : v;
    } catch { return "name"; }
  });
  const setSortBy = (v: string) => {
    setSortByState(v);
    try { localStorage.setItem("gs:sortBy", v); } catch {}
  };

  async function refresh() {
    try {
      const list = await api.ListGames();
      setGames((list ?? []) as any);
    } catch (e) { console.error(e); }
  }

  useEffect(() => {
    api.GetConfig().then((c: any) => { if (c?.language) setLanguage(c.language); });
    refresh();

    const offProg = EventsOn("scan:progress", (p: any) => setPhase(p?.phase + (p?.name ? ": " + p.name : "")));
    const offSrc = EventsOn("scan:source-done", () => {});
    const offGame = EventsOn("scan:game", () => { refresh(); });
    const offMatch = EventsOn("match:game", () => { refresh(); });
    const offMeta = EventsOn("meta:game", () => { refresh(); });
    const offDone = EventsOn("scan:done", (r: any) => {
      // Pipeline (launchers + heuristic) finished — match + reverse still run after.
      setPhase("matching & enriching…");
      api.Toast("info", t("toast.scanDone") + ` (${r?.gamesUpserted ?? 0} games / ${r?.installationsTotal ?? 0} installs) — ищу сейвы…`);
      refresh();
    });
    const offRevProg = EventsOn("reverse:progress", (p: any) => {
      setPhase(`reverse: ${p?.checked ?? 0} проверено, ${p?.matched ?? 0} совпало, ${p?.created ?? 0} новых`);
    });
    const offRevDone = EventsOn("reverse:done", (r: any) => {
      // Whole pipeline done.
      setScanning(false);
      setScanned(true);
      setPhase("");
      api.Toast("success",
        `Готово · ${r?.entriesMatched ?? 0} сейв-совпадений, ${r?.newGamesCreated ?? 0} новых игр, ${r?.newLocationsAdded ?? 0} новых сейвов`);
      refresh();
    });
    const offUpdate = EventsOn("update:available", (info: any) => {
      setUpdate(info as UpdateInfo);
    });
    const offInstSize = EventsOn("inst:size", () => { refresh(); });
    const offReconcile = EventsOn("reconcile:done", (r: any) => {
      const created = (r?.createdGames ?? 0) + (r?.createdLocations ?? 0);
      const msg = `Бэкапы синхронизированы: импорт ${r?.importedSnapshots ?? 0}` +
        (created > 0 ? `, создано записей ${created}` : "") +
        (r?.deadSnapshots ? `, удалено мёртвых ${r.deadSnapshots}` : "");
      api.Toast("info", msg);
      refresh();
    });
    return () => {
      try { (offProg as any)?.(); (offSrc as any)?.(); (offGame as any)?.(); (offMatch as any)?.(); (offMeta as any)?.(); (offDone as any)?.(); (offRevProg as any)?.(); (offRevDone as any)?.(); (offReconcile as any)?.(); (offUpdate as any)?.(); (offInstSize as any)?.(); } catch {}
    };
  }, []);

  // Re-open drawer with fresh data on refresh
  useEffect(() => {
    if (!opened) return;
    const updated = games.find((g) => g.game.id === opened.game.id);
    if (updated) setOpened(updated);
  }, [games]);

  async function onScan() {
    setScanning(true);
    setPhase("starting…");
    try {
      await api.ScanLibrary();
      // ScanLibrary runs pipeline → match → reverse → enrich synchronously.
      // The reverse:done event will clear scanning state.
    } catch (e) {
      console.error(e);
      setScanning(false);
      setPhase("");
      api.Toast("error", t("toast.scanFailed"));
    }
  }
  async function onBackupAll() {
    api.Toast("info", t("toast.backupStart"));
    try { const n = await api.BackupAll(); api.Toast("success", t("toast.backupDone") + ` (${n})`); refresh(); }
    catch (e) { console.error(e); }
  }
  async function onOpenBackups() { try { await api.OpenBackupFolder(); } catch (e) { console.error(e); } }

  const totalCount = games.length;

  return (
    <div className="flex h-screen w-screen text-gray-100">
      <Sidebar page={page} onNavigate={setPage} />
      <main className="flex flex-1 flex-col overflow-hidden">
        {update && update.available && (
          <UpdateBanner info={update} onDismiss={() => setUpdate(null)} />
        )}
        {(page === "dashboard" || page === "library") && (
          <TopBar
            scanning={scanning}
            onScan={onScan}
            onBackupAll={onBackupAll}
            onOpenBackups={onOpenBackups}
            searchValue={query}
            onSearchChange={setQuery}
            filter={filter}
            onFilterChange={setFilter}
            sortBy={sortBy}
            onSortByChange={setSortBy}
          />
        )}
        {phase && (
          <div className="border-b border-border bg-panel/80 px-5 py-1 text-xs text-muted">
            ⏳ {phase}
          </div>
        )}
        <div className="flex-1 overflow-y-auto">
          {page === "dashboard" && (
            <DashboardPage
              games={games}
              onOpen={setOpened}
              query={query}
              filter={filter}
              sortBy={sortBy}
              scanned={scanned || totalCount > 0}
            />
          )}
          {page === "library" && <LibraryPage games={games} onOpen={setOpened} />}
          {page === "backups" && <BackupsPage games={games} />}
          {page === "settings" && <SettingsPage />}
        </div>
      </main>
      {opened && <GameDrawer view={opened} onClose={() => setOpened(null)} onChanged={refresh} />}
      <Toaster />
    </div>
  );
}
