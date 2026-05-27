import { useEffect, useState } from "react";
import { api, type AppConfig } from "../api";
import { setLanguage, useT } from "../i18n";

export function SettingsPage() {
  const t = useT();
  const [cfg, setCfg] = useState<AppConfig | null>(null);
  const [key, setKey] = useState("");
  const [reconciling, setReconciling] = useState(false);

  useEffect(() => {
    api.GetConfig().then((c: any) => {
      setCfg(c);
      setKey(c?.steamGridDbKey ?? "");
      if (c?.language) setLanguage(c.language);
    });
  }, []);

  async function pickBackup() {
    const p = await api.PickFolder("Pick a folder for backups");
    if (p && cfg) {
      await api.SetBackupRoot(p);
      setCfg({ ...cfg, backupRoot: p });
    }
  }

  async function saveKey() {
    await api.SetSteamGridDBKey(key);
    if (cfg) setCfg({ ...cfg, steamGridDbKey: key });
  }

  async function changeLang(l: string) {
    await api.SetLanguage(l);
    setLanguage(l);
    if (cfg) setCfg({ ...cfg, language: l });
  }

  async function reconcileBackups() {
    setReconciling(true);
    try {
      const r: any = await api.ReconcileBackups();
      api.Toast("success",
        `Reconcile: проверено ${r?.scannedManifests ?? 0}, импортировано ${r?.importedSnapshots ?? 0}, ` +
        `уже было ${r?.alreadyKnown ?? 0}, создано игр ${r?.createdGames ?? 0}, ` +
        `сейв-папок ${r?.createdLocations ?? 0}, удалено мёртвых ${r?.deadSnapshots ?? 0}` +
        (r?.orphanZips ? `, orphan-zip без манифеста ${r.orphanZips}` : ""));
    } catch (e) {
      api.Toast("error", "Reconcile failed: " + String(e));
    } finally {
      setReconciling(false);
    }
  }

  async function checkForUpdate() {
    try {
      const info: any = await api.CheckForUpdate();
      if (info?.available) {
        api.Toast("success", `Доступна ${info.latestVersion}. Баннер появится сверху.`);
      } else {
        api.Toast("info", `Это последняя версия (${info?.currentVersion ?? "?"}).`);
      }
    } catch (e) {
      api.Toast("error", "Проверка обновления: " + String(e));
    }
  }
  async function toggleAutoCheck(v: boolean) {
    await api.SetAutoCheckUpdates(v);
    if (cfg) setCfg({ ...cfg, autoCheckUpdates: v });
  }

  if (!cfg) return <div className="p-6 text-muted">Loading…</div>;

  return (
    <div className="mx-auto max-w-2xl space-y-5 p-6">
      <section className="card p-4">
        <div className="text-xs uppercase tracking-wide text-muted">{t("settings.backupRoot")}</div>
        <div className="mt-1 flex items-center gap-2">
          <input className="input flex-1" value={cfg.backupRoot} readOnly />
          <button className="btn btn-primary" onClick={pickBackup}>{t("actions.pickFolder")}</button>
        </div>
        <div className="mt-3 flex items-center gap-2">
          <button
            className="btn"
            disabled={reconciling}
            onClick={reconcileBackups}
            title="Просканировать папку бэкапов на диске и пересинхронизировать БД (импортирует zip-ы у которых нет строк в БД, чистит мёртвые ссылки на удалённые файлы)"
          >
            {reconciling ? "↻ Reconcile…" : "🔄 Reconcile backups"}
          </button>
          <span className="text-xs text-muted">
            Перечитать диск и пересоздать строки snapshots для всех найденных zip+manifest
          </span>
        </div>
      </section>

      <section className="card p-4">
        <div className="text-xs uppercase tracking-wide text-muted">{t("settings.steamGridDbKey")}</div>
        <div className="mt-1 flex items-center gap-2">
          <input
            className="input flex-1"
            type="password"
            value={key}
            onChange={(e) => setKey(e.target.value)}
            placeholder="оставь пустым, чтобы использовать встроенный ключ"
          />
          <button className="btn btn-primary" onClick={saveKey}>{t("actions.save")}</button>
        </div>
        <p className="mt-2 text-xs text-muted">
          В release-сборках ключ зашит в бинарь через CI-секрет, ввод не требуется.
          Личный ключ можно получить здесь: <a
            href="https://www.steamgriddb.com/profile/preferences/api"
            target="_blank"
            rel="noreferrer"
            className="text-accent underline decoration-dotted underline-offset-2 hover:text-accent2"
            onClick={(e) => {
              e.preventDefault();
              window.open("https://www.steamgriddb.com/profile/preferences/api", "_blank");
            }}
          >
            steamgriddb.com/profile/preferences/api
          </a>. Свой ключ перебивает встроенный (выше rate-limit, изолирован от остальных пользователей).
        </p>
      </section>

      <section className="card p-4">
        <div className="text-xs uppercase tracking-wide text-muted">{t("settings.language")}</div>
        <div className="mt-2 flex gap-2">
          {["ru", "en"].map((l) => (
            <button
              key={l}
              className={"btn " + (cfg.language === l ? "btn-primary" : "")}
              onClick={() => changeLang(l)}
            >
              {l.toUpperCase()}
            </button>
          ))}
        </div>
      </section>

      <section className="card p-4">
        <div className="text-xs uppercase tracking-wide text-muted">{t("settings.retention")}</div>
        <div className="mt-1 flex items-center gap-2">
          <input
            type="number"
            min={0}
            max={1000}
            step={1}
            className="input w-24"
            value={cfg.retentionKeepN}
            onChange={async (e) => {
              const n = Math.max(0, Math.min(1000, parseInt(e.target.value || "0", 10)));
              await api.SetRetentionKeepN(n);
              setCfg({ ...cfg, retentionKeepN: n });
            }}
          />
          <span className="text-xs text-muted">снэпшотов на одну папку сейва (0 = без лимита)</span>
        </div>
        <p className="mt-2 text-xs text-muted">Старые сверх лимита удаляются автоматически после каждого нового бэкапа.</p>
      </section>

      <section className="card p-4">
        <div className="text-xs uppercase tracking-wide text-muted">Auto-backup (watcher)</div>
        <label className="mt-2 flex items-center gap-2 text-sm text-gray-200">
          <input
            type="checkbox"
            checked={cfg.watcherEnabled}
            onChange={async (e) => {
              const v = e.target.checked;
              await api.WatcherToggle(v);
              setCfg({ ...cfg, watcherEnabled: v });
            }}
          />
          Следить за изменениями сейвов и автоматически бэкапить
        </label>
        <div className="mt-3 flex items-center gap-2">
          <span className="text-sm text-gray-300">Бэкап через</span>
          <input
            type="number"
            min={1}
            max={1440}
            step={1}
            className="input w-20"
            value={Math.max(1, Math.round((cfg.watcherDebounceMs || 600000) / 60000))}
            onChange={async (e) => {
              const m = Math.max(1, Math.min(1440, parseInt(e.target.value || "10", 10)));
              await api.SetWatcherDebounceMinutes(m);
              setCfg({ ...cfg, watcherDebounceMs: m * 60 * 1000 });
            }}
          />
          <span className="text-sm text-gray-300">минут тишины после последнего изменения</span>
        </div>
        <p className="mt-2 text-xs text-muted">
          При запущенной игре авто-бэкап откладывается (файлы могут быть открыты).
          Включается также из иконки в трее (правой кнопкой → Auto-backup).
        </p>
      </section>

      <section className="card p-4">
        <div className="text-xs uppercase tracking-wide text-muted">Обновления</div>
        <label className="mt-2 flex items-center gap-2 text-sm text-gray-200">
          <input
            type="checkbox"
            checked={cfg.autoCheckUpdates !== false}
            onChange={(e) => toggleAutoCheck(e.target.checked)}
          />
          Автоматически проверять обновления при запуске
        </label>
        <div className="mt-3 flex items-center gap-2">
          <button className="btn" onClick={checkForUpdate}>🔄 Проверить сейчас</button>
          <span className="text-xs text-muted">
            Тянет latest release из github.com/LastSkywalkerER/GameSaver
          </span>
        </div>
      </section>
    </div>
  );
}
