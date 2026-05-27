import { useEffect, useState } from "react";
import { api, type AppConfig } from "../api";
import { setLanguage, useT } from "../i18n";
import { TILE_PREF_LABELS, setTilePref, useTilePrefs, type TilePrefs } from "../tilePrefs";
import { confirmModal } from "../components/Modal";

type ShellStatus = {
  watchdogPresent: boolean;
  registered: boolean;
  runningAsShell: boolean;
};

export function SettingsPage() {
  const t = useT();
  const [cfg, setCfg] = useState<AppConfig | null>(null);
  const [key, setKey] = useState("");
  const [reconciling, setReconciling] = useState(false);
  const [shell, setShell] = useState<ShellStatus | null>(null);
  const [shellBusy, setShellBusy] = useState(false);
  const tilePrefs = useTilePrefs();

  useEffect(() => {
    api.GetConfig().then((c: any) => {
      setCfg(c);
      setKey(c?.steamGridDbKey ?? "");
      if (c?.language) setLanguage(c.language);
    });
    refreshShellStatus();
  }, []);

  async function refreshShellStatus() {
    try {
      const s: any = await api.GetShellModeStatus();
      setShell(s);
    } catch (e) { console.error(e); }
  }

  async function enableShellMode() {
    const ok = await confirmModal({
      title: "Включить режим оболочки?",
      variant: "danger",
      confirmLabel: "Включить",
      body: (
        <ul className="list-disc space-y-1 pl-5">
          <li>После следующего входа в Windows загрузится ТОЛЬКО GameSaver — без панели задач, меню Пуск, иконок рабочего стола и системного трея.</li>
          <li>Системный трей перестанет работать (он живёт на Explorer).</li>
          <li>Если GameSaver упадёт несколько раз подряд, watchdog сам отключит режим и вернёт Explorer.</li>
          <li>Аварийный выход: <kbd className="rounded border border-border bg-card px-1">Ctrl+Alt+Shift+F12</kbd> в любой момент.</li>
          <li>Также можно выйти из режима кнопкой в Настройках без перезагрузки.</li>
          <li className="pt-1 text-muted">При первом включении watchdog (~2 МБ) скачается с GitHub Releases в <code className="rounded bg-card px-1">%LOCALAPPDATA%\GameSaver\bin\</code>.</li>
        </ul>
      ),
    });
    if (!ok) return;
    setShellBusy(true);
    try {
      await api.EnableShellMode();
      api.Toast("success", "Shell mode включён. Перезагрузка применит изменения.");
      await refreshShellStatus();
    } catch (e) {
      api.Toast("error", "Не удалось включить shell mode: " + String(e));
    } finally {
      setShellBusy(false);
    }
  }

  async function disableShellMode() {
    setShellBusy(true);
    try {
      await api.DisableShellMode();
      // If we're CURRENTLY running under the watchdog, also quit ourselves:
      // watchdog sees exit 0, declines to restart, launches explorer.exe —
      // user gets the desktop back without having to sign out. In normal
      // mode (registry was set but we're under explorer, e.g. user enabled
      // and disabled without re-logging) just refresh the toggle state.
      if (shell?.runningAsShell) {
        api.Toast("success", "Возвращаем Explorer…");
        await api.QuitApp();
        return; // we're about to die; UI cleanup happens on the next session
      }
      api.Toast("success", "Shell mode выключен. Следующий вход вернёт обычный рабочий стол.");
      await refreshShellStatus();
    } catch (e) {
      api.Toast("error", "Не удалось выключить shell mode: " + String(e));
    } finally {
      setShellBusy(false);
    }
  }

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
        <div className="text-xs uppercase tracking-wide text-muted">Содержимое плиток</div>
        <p className="mt-1 text-xs text-muted">Что отображать на карточке игры в дашборде:</p>
        <div className="mt-2 grid grid-cols-1 gap-1.5 sm:grid-cols-2">
          {(Object.keys(TILE_PREF_LABELS) as (keyof TilePrefs)[]).map((k) => (
            <label key={k} className="flex items-center gap-2 text-sm text-gray-200">
              <input
                type="checkbox"
                checked={tilePrefs[k]}
                onChange={(e) => setTilePref(k, e.target.checked)}
              />
              {TILE_PREF_LABELS[k]}
            </label>
          ))}
        </div>
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

      <section className="card p-4">
        <div className="text-xs uppercase tracking-wide text-muted">
          Авто-вход Windows (без пароля)
        </div>
        <p className="mt-2 text-xs text-muted">
          Пароль на учётке останется, но Windows перестанет спрашивать его при
          входе. Полезно для shell-режима — система сама загрузится в
          GameSaver без участия клавиатуры. Жмёшь кнопку → откроется
          стандартное окно <code className="rounded bg-card px-1">netplwiz</code>{" "}
          → снимаешь галочку <em>«Users must enter a user name and password…»</em>{" "}
          → вводишь пароль (Windows сохранит его в зашифрованном LSA, не в
          реестре).
          {" "}На Windows 11 22H2+ галочка скрыта по умолчанию — мы её включим
          через одну UAC-подсказку.
        </p>
        <div className="mt-3">
          <button
            className="btn"
            onClick={async () => {
              try {
                await api.OpenAutoLoginConfigurator();
                api.Toast("info", "Открыли netplwiz — снимай галочку и подтверждай.");
              } catch (e) {
                api.Toast("error", "Не удалось открыть: " + String(e));
              }
            }}
          >
            🔓 Настроить авто-вход
          </button>
        </div>
      </section>

      <section className="card border-red-700/40 p-4">
        <div className="text-xs uppercase tracking-wide text-red-300">
          ⚠ Режим оболочки (shell replacement)
        </div>
        <p className="mt-2 text-xs text-muted">
          Заменяет Explorer на GameSaver при входе в Windows. Получишь
          Big-Picture-подобный лаунчер без рабочего стола, панели задач, меню Пуск
          и системного трея. Watchdog (~2 МБ) скачается с GitHub Releases в{" "}
          <code className="rounded bg-card px-1">%LOCALAPPDATA%\GameSaver\bin\</code>{" "}
          и сам перезапустит прилу при падении. Аварийный выход —{" "}
          <kbd className="rounded border border-border bg-card px-1">
            Ctrl+Alt+Shift+F12
          </kbd>.
        </p>
        <div className="mt-3 flex items-center gap-3">
          {shell?.registered ? (
            <button
              className="btn btn-primary"
              disabled={shellBusy}
              onClick={disableShellMode}
            >
              {shellBusy ? "…" : "🛑 Выйти из shell-режима"}
            </button>
          ) : (
            <button
              className="btn"
              disabled={shellBusy}
              onClick={enableShellMode}
            >
              {shellBusy ? "↻ Включаем…" : "🖥 Включить shell-режим"}
            </button>
          )}
          <div className="text-xs text-muted">
            {shell?.runningAsShell && (
              <span className="text-amber-300">Сейчас под watchdog'ом. </span>
            )}
            {shell?.registered
              ? "Зарегистрирован: следующий вход в Windows запустит GameSaver вместо Explorer."
              : "Не зарегистрирован: обычный рабочий стол."}
            {!shell?.watchdogPresent && " Watchdog ещё не скачан."}
          </div>
        </div>
      </section>
    </div>
  );
}
