import clsx from "clsx";
import { useEffect, useState } from "react";
import { api } from "../api";
import { useT } from "../i18n";
import { useControllerConnected } from "../controller";

// "library" is gone — it was a near-duplicate of dashboard. If we ever
// bring back a "flat list" view it can have its own page key.
export type Page = "dashboard" | "backups" | "settings";

const COLLAPSE_KEY = "gs:sidebarCollapsed";

export function Sidebar({
  page,
  onNavigate,
}: {
  page: Page;
  onNavigate: (p: Page) => void;
}) {
  const t = useT();
  const [version, setVersion] = useState<string>("");
  const padOn = useControllerConnected();
  // Collapse state persists across sessions — power users who collapse it
  // once want it to stay that way.
  const [collapsed, setCollapsedState] = useState<boolean>(() => {
    try { return localStorage.getItem(COLLAPSE_KEY) === "1"; } catch { return false; }
  });
  const setCollapsed = (v: boolean) => {
    setCollapsedState(v);
    try { localStorage.setItem(COLLAPSE_KEY, v ? "1" : "0"); } catch {}
  };

  useEffect(() => {
    api.AppVersion().then((v: any) => setVersion(typeof v === "string" ? v : ""));
  }, []);

  const items: { id: Page; label: string; icon: string }[] = [
    { id: "dashboard", label: t("nav.dashboard"), icon: "▦" },
    { id: "backups",   label: t("nav.backups"),   icon: "⛁" },
    { id: "settings",  label: t("nav.settings"),  icon: "⚙" },
  ];

  return (
    <aside
      className={clsx(
        "flex shrink-0 flex-col border-r border-border bg-panel transition-[width] duration-200",
        collapsed ? "w-14" : "w-56"
      )}
    >
      {/* Collapse / expand toggle. Sits where the GS logo block used to live —
          no app title there anymore since the OS already shows it in the
          window chrome. */}
      <button
        onClick={() => setCollapsed(!collapsed)}
        className="mx-2 mt-3 flex items-center justify-center rounded-lg border border-border bg-card px-2 py-2 text-sm text-gray-300 transition hover:bg-[#262936]"
        title={collapsed ? "Развернуть боковую панель" : "Свернуть боковую панель"}
      >
        {collapsed ? "»" : "«"}
      </button>

      <nav className="mt-3 flex-1 px-2">
        {items.map((it) => (
          <button
            key={it.id}
            onClick={() => onNavigate(it.id)}
            title={collapsed ? it.label : undefined}
            className={clsx(
              "mb-1 flex w-full items-center gap-3 rounded-lg px-3 py-2 text-left text-sm transition",
              page === it.id ? "bg-accent/15 text-accent" : "text-gray-300 hover:bg-card",
              collapsed && "justify-center px-0"
            )}
          >
            <span className="w-5 text-center text-base">{it.icon}</span>
            {!collapsed && <span>{it.label}</span>}
          </button>
        ))}
      </nav>

      {padOn && (
        <div
          className={clsx(
            "mx-3 mb-2 flex items-center justify-center gap-1 rounded-md border border-emerald-700/50 bg-emerald-900/40 text-[11px] text-emerald-200",
            collapsed ? "px-1 py-1" : "px-2 py-1"
          )}
          title="Xbox-совместимый контроллер подключён. D-pad/левый стик — навигация, A — открыть, B — назад, Start — следующая страница"
        >
          🎮 {!collapsed && "controller"}
        </div>
      )}

      {!collapsed && (
        <div
          className="px-4 py-3 text-[11px] text-muted"
          title="Версия пробивается в бинарь при сборке (ldflags). 'dev' = локальная сборка"
        >
          {version || "…"}
        </div>
      )}
    </aside>
  );
}
