import { useEffect, useState } from "react";
import { api, EventsOn, type GameView, type UpdateInfo } from "./api";
import { Sidebar, type Page } from "./components/Sidebar";
import { TopBar } from "./components/TopBar";
import { Toaster } from "./components/Toaster";
import { GameDrawer } from "./components/GameDrawer";
import { UpdateBanner } from "./components/UpdateBanner";
import { DashboardPage } from "./pages/DashboardPage";
import { BackupsPage } from "./pages/BackupsPage";
import { SettingsPage } from "./pages/SettingsPage";
import { setLanguage, useT } from "./i18n";
import { useControllerButton } from "./controller";
import { ShellApp } from "./components/shell/ShellApp";

export default function App() {
  const t = useT();
  const [page, setPage] = useState<Page>("dashboard");
  const [games, setGames] = useState<GameView[]>([]);
  // Are we currently running under gamesaver-watchdog.exe as the user
  // shell? If yes we render the immersive ShellApp instead of the regular
  // sidebar + topbar UI. Null = still polling backend; renders nothing
  // for one frame to avoid a flash of the wrong UI.
  const [shellMode, setShellMode] = useState<boolean | null>(null);
  const [scanning, setScanning] = useState(false);
  const [scanned, setScanned] = useState(false);
  const [query, setQuery] = useState("");
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
    api.GetShellModeStatus().then((s: any) => setShellMode(!!s?.runningAsShell)).catch(() => setShellMode(false));
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

  // Controller global actions: Start cycles top-level pages, B closes drawer.
  // Per-page navigation (d-pad + A) is handled inside the relevant page.
  // In shell mode the entire desktop UI is replaced by ShellApp, which has
  // its own controller bindings — short-circuit here to avoid double-handling.
  const pages: Page[] = ["dashboard", "backups", "settings"];
  useControllerButton((btn) => {
    if (shellMode) return;
    if (btn === "b" && opened) {
      setOpened(null);
      return;
    }
    if (btn === "start") {
      setPage((p) => {
        const idx = pages.indexOf(p);
        return pages[(idx + 1) % pages.length];
      });
    }
  });

  const totalCount = games.length;

  // Block the first frame until we know whether we're shell — otherwise the
  // regular desktop UI flashes for ~50 ms before swapping out.
  if (shellMode === null) {
    return <div className="h-screen w-screen bg-bg" />;
  }
  if (shellMode) {
    return (
      <>
        <ShellApp games={games} refresh={refresh} />
        <Toaster />
      </>
    );
  }

  return (
    <div className="flex h-screen w-screen text-gray-100">
      <Sidebar page={page} onNavigate={setPage} />
      <main className="flex flex-1 flex-col overflow-hidden">
        {update && update.available && (
          <UpdateBanner info={update} onDismiss={() => setUpdate(null)} />
        )}
        {page === "dashboard" && (
          <TopBar
            scanning={scanning}
            onScan={onScan}
            onBackupAll={onBackupAll}
            onOpenBackups={onOpenBackups}
            searchValue={query}
            onSearchChange={setQuery}
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
              sortBy={sortBy}
              scanned={scanned || totalCount > 0}
            />
          )}
          {page === "backups" && <BackupsPage games={games} />}
          {page === "settings" && <SettingsPage />}
        </div>
      </main>
      {opened && <GameDrawer view={opened} onClose={() => setOpened(null)} onChanged={refresh} />}
      <Toaster />
    </div>
  );
}
