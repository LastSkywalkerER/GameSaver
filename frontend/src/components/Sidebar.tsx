import clsx from "clsx";
import { useEffect, useState } from "react";
import { api } from "../api";
import { useT } from "../i18n";

export type Page = "dashboard" | "library" | "backups" | "settings";

export function Sidebar({
  page,
  onNavigate,
}: {
  page: Page;
  onNavigate: (p: Page) => void;
}) {
  const t = useT();
  const [version, setVersion] = useState<string>("");
  useEffect(() => {
    api.AppVersion().then((v: any) => setVersion(typeof v === "string" ? v : ""));
  }, []);
  const items: { id: Page; label: string; icon: string }[] = [
    { id: "dashboard", label: t("nav.dashboard"), icon: "▦" },
    { id: "library",   label: t("nav.library"),   icon: "≡" },
    { id: "backups",   label: t("nav.backups"),   icon: "⛁" },
    { id: "settings",  label: t("nav.settings"),  icon: "⚙" },
  ];
  return (
    <aside className="flex w-56 shrink-0 flex-col border-r border-border bg-panel">
      <div className="px-4 py-5">
        <div className="flex items-center gap-2">
          <div className="grid h-8 w-8 place-items-center rounded-lg bg-accent text-white">GS</div>
          <div className="text-lg font-semibold tracking-tight">{t("app.title")}</div>
        </div>
      </div>
      <nav className="flex-1 px-2">
        {items.map((it) => (
          <button
            key={it.id}
            onClick={() => onNavigate(it.id)}
            className={clsx(
              "mb-1 flex w-full items-center gap-3 rounded-lg px-3 py-2 text-left text-sm transition",
              page === it.id ? "bg-accent/15 text-accent" : "text-gray-300 hover:bg-card"
            )}
          >
            <span className="w-5 text-center text-base">{it.icon}</span>
            <span>{it.label}</span>
          </button>
        ))}
      </nav>
      <div
        className="px-4 py-3 text-[11px] text-muted"
        title="Версия пробивается в бинарь при сборке (ldflags). 'dev' = локальная сборка"
      >
        {version || "…"}
      </div>
    </aside>
  );
}
