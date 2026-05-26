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
          <input className="input flex-1" type="password" value={key} onChange={(e) => setKey(e.target.value)} placeholder="api key (optional)" />
          <button className="btn btn-primary" onClick={saveKey}>{t("actions.save")}</button>
        </div>
        <p className="mt-2 text-xs text-muted">
          Без ключа обложки тянутся только из Steam CDN (для Steam-игр).
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
        <div className="mt-1 text-sm text-gray-200">{cfg.retentionKeepN}</div>
        <p className="mt-2 text-xs text-muted">Старые снэпшоты сверх лимита удаляются автоматически.</p>
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
