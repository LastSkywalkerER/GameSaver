import { useEffect, useState } from "react";
import { api, coverUrl, formatBytes, formatDate, formatDuration, formatRelative, type GameView, type ManifestSearchResult, type PlaySession } from "../api";
import { SourceBadge } from "./SourceBadge";
import { useT } from "../i18n";
import { ManifestPickerDialog } from "./ManifestPicker";
import { DeepScanDialog } from "./DeepScanDialog";
import { confirmModal } from "./Modal";

export function GameDrawer({
  view,
  onClose,
  onChanged,
}: {
  view: GameView;
  onClose: () => void;
  onChanged: () => void;
}) {
  const t = useT();
  const [busy, setBusy] = useState<string | null>(null);
  const [showPicker, setShowPicker] = useState(false);
  const [showDeepScan, setShowDeepScan] = useState(false);
  const [sessions, setSessions] = useState<PlaySession[]>([]);
  const hero = coverUrl(view.game.heroPath) ?? coverUrl(view.game.coverPath);

  // Re-fetch sessions whenever the drawer opens a different game or the game
  // view refreshes (e.g. after a backup).
  useEffect(() => {
    let cancelled = false;
    api.ListPlaySessions(view.game.id, 50).then((s: any) => {
      if (!cancelled) setSessions((s ?? []) as PlaySession[]);
    }).catch(() => {});
    return () => { cancelled = true; };
  }, [view.game.id, view.game.lastPlayedAt, view.game.totalPlaySeconds]);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape" && !showPicker && !showDeepScan) onClose();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose, showPicker, showDeepScan]);

  async function doBackup() {
    setBusy("backup");
    try {
      await api.BackupGame(view.game.id);
      onChanged();
    } finally { setBusy(null); }
  }
  async function doLaunch(installID: string) {
    setBusy("launch");
    try {
      await api.LaunchGame(view.game.id, installID);
    } catch (e) {
      api.Toast("error", "Не удалось запустить: " + String(e));
    } finally { setBusy(null); }
  }
  async function doRestore(snapID: string) {
    const ok = await confirmModal({
      title: "Восстановить сейв?",
      body: "Текущее состояние будет автоматически забэкаплено перед перезаписью.",
      confirmLabel: "Восстановить",
    });
    if (!ok) return;
    setBusy("restore");
    try {
      await api.RestoreSnapshot(snapID, true);
      onChanged();
    } finally { setBusy(null); }
  }
  async function doDeleteSnap(snapID: string) {
    const ok = await confirmModal({
      title: "Удалить снэпшот навсегда?",
      body: "Архив будет стёрт с диска без возможности восстановления.",
      confirmLabel: "Удалить",
      variant: "danger",
    });
    if (!ok) return;
    setBusy("delete");
    try {
      await api.DeleteSnapshot(snapID);
      onChanged();
    } finally { setBusy(null); }
  }
  async function doRemoveSave(locID: string) {
    const ok = await confirmModal({
      title: "Открепить папку сейва?",
      body: "Папка отвяжется от этой игры в базе. Файлы на диске не трогаются.",
      confirmLabel: "Открепить",
    });
    if (!ok) return;
    setBusy("rmloc");
    try {
      await api.RemoveSaveLocation(locID);
      onChanged();
    } finally { setBusy(null); }
  }
  async function doAddManualSave() {
    setBusy("addsave");
    try {
      const path = await api.PickFolder("Выбери папку с сейвами");
      if (path) {
        await api.AddManualSaveLocation(view.game.id, path);
        onChanged();
        api.Toast("success", "Папка сейва добавлена");
      }
    } catch (e) {
      api.Toast("error", "Не удалось добавить: " + String(e));
    } finally { setBusy(null); }
  }
  async function doPicked(entry: ManifestSearchResult | null) {
    setShowPicker(false);
    setBusy("rematch");
    try {
      if (entry === null) {
        await api.ClearManifestOverride(view.game.id);
        api.Toast("success", "Override сброшен");
      } else {
        await api.RematchGame(view.game.id, entry.key);
        api.Toast("success", "Привязано к " + entry.name);
      }
      onChanged();
    } catch (e) {
      api.Toast("error", "Не удалось перепривязать: " + String(e));
    } finally { setBusy(null); }
  }

  return (
    <div className="fixed inset-0 z-40 flex justify-end bg-black/60 backdrop-blur-sm" onClick={onClose}>
      <div
        className="card relative h-full w-full max-w-3xl overflow-y-auto rounded-none border-l border-border"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="relative h-56 overflow-hidden">
          {hero ? (
            <img src={hero} alt="" className="absolute inset-0 h-full w-full object-cover" />
          ) : (
            <div className="absolute inset-0 bg-gradient-to-br from-accent/30 to-accent2/20" />
          )}
          <div className="absolute inset-0 bg-gradient-to-t from-panel via-panel/70 to-transparent" />
          <button onClick={onClose} className="absolute right-3 top-3 btn btn-ghost text-gray-300">✕</button>
          <div className="absolute bottom-3 left-5 right-5">
            <h2 className="text-2xl font-semibold leading-tight">{view.game.name}</h2>
            <div className="mt-2 flex flex-wrap gap-1">
              {Array.from(new Set(view.installations.map((i) => i.source))).map((s) => (
                <SourceBadge key={s} source={s} />
              ))}
              {view.game.steamAppId ? (
                <span className="chip">AppID {view.game.steamAppId}</span>
              ) : null}
              {view.game.lastPlayedAt ? (
                <span className="chip" title={"Last played: " + formatDate(view.game.lastPlayedAt)}>
                  ▶ {formatRelative(view.game.lastPlayedAt)}
                </span>
              ) : null}
              {view.game.totalPlaySeconds && view.game.totalPlaySeconds > 0 ? (
                <span className="chip" title="Сумма по всем сессиям">
                  ⏱ {formatDuration(view.game.totalPlaySeconds)}
                </span>
              ) : null}
            </div>
          </div>
        </div>

        <div className="space-y-6 p-5">
          {/* Quick actions toolbar */}
          <div className="flex flex-wrap gap-2">
            <button className="btn" disabled={busy !== null} onClick={() => setShowPicker(true)} title="Перепривязать к другой записи Ludusavi (правильная игра)">
              🔄 Перевыбрать игру
            </button>
            <button className="btn" disabled={busy !== null} onClick={() => setShowDeepScan(true)} title="Поискать сейвы глубже по Saved Games / My Games / AppData">
              🔍 Глубокий скан
            </button>
            <button className="btn" disabled={busy === "addsave"} onClick={doAddManualSave} title="Указать папку сейва вручную">
              ➕ Папка сейва
            </button>
          </div>

          {/* Installations */}
          <section>
            <h3 className="mb-2 text-sm font-semibold uppercase tracking-wide text-muted">{t("drawer.installations")}</h3>
            {view.installations.length === 0 && <p className="text-sm text-muted">{t("drawer.noInstallations")}</p>}
            <div className="grid gap-2">
              {view.installations.map((inst) => {
                const dirSize = inst.installDirSizeBytes ?? 0;
                const measured = (inst.installDirSizeAt ?? 0) > 0;
                return (
                  <div key={inst.id} className="card flex items-center justify-between gap-3 px-3 py-2">
                    <div className="min-w-0 flex-1">
                      <div className="flex items-center gap-2 text-xs">
                        <SourceBadge source={inst.source} />
                        {dirSize > 0 ? (
                          <span
                            className="chip border-amber-700/50 bg-amber-900/40 text-amber-200"
                            title={"Размер папки игры: " + formatBytes(dirSize)}
                          >
                            💾 {formatBytes(dirSize)}
                          </span>
                        ) : measured ? (
                          <span className="chip">пусто</span>
                        ) : (
                          <span className="chip text-muted" title="Размер ещё не подсчитан — фоновый walker в работе">…</span>
                        )}
                      </div>
                      <div className="mt-1 truncate text-sm text-gray-200" title={inst.rootPath}>{inst.rootPath}</div>
                      <div className="truncate text-xs text-muted" title={inst.exePath}>{inst.exePath}</div>
                    </div>
                    <div className="flex items-center gap-2">
                      <button className="btn" onClick={() => api.ShowItemInFolder(inst.rootPath).catch((e) => api.Toast("error", "Открыть папку: " + String(e)))}>📁</button>
                      <button className="btn btn-primary" disabled={busy === "launch"} onClick={() => doLaunch(inst.id)}>
                        ▶ {t("actions.play")}
                      </button>
                    </div>
                  </div>
                );
              })}
            </div>
          </section>

          {/* Save locations */}
          <section>
            <div className="mb-2 flex items-center justify-between">
              <h3 className="text-sm font-semibold uppercase tracking-wide text-muted">{t("drawer.saveLocations")}</h3>
              <button className="btn btn-primary" disabled={busy === "backup" || view.saveLocations.length === 0} onClick={doBackup}>
                ⛁ {t("actions.backup")}
              </button>
            </div>
            {view.saveLocations.length === 0 && <p className="text-sm text-muted">{t("drawer.noSaves")}</p>}
            <div className="grid gap-2">
              {view.saveLocations.map((loc) => (
                <div key={loc.id} className="card px-3 py-2">
                  <div className="flex items-start justify-between gap-3">
                    <div className="min-w-0 flex-1">
                      <div className="flex items-center gap-2 text-xs">
                        <span className="chip">{loc.kind}</span>
                        {loc.sourceHint && <span className="chip">{loc.sourceHint}</span>}
                        {loc.isJunction && <span className="chip">junction</span>}
                      </div>
                      <div className="mt-1 truncate text-sm text-gray-200" title={loc.path}>{loc.path}</div>
                      <div className="mt-0.5 text-xs text-muted">
                        {loc.fileCount} files · {formatBytes(loc.sizeBytes)} · {formatDate(loc.mtime)}
                      </div>
                    </div>
                    <div className="flex items-center gap-1">
                      <button className="btn" onClick={() => api.ShowItemInFolder(loc.path).catch((e) => api.Toast("error", "Открыть папку: " + String(e)))}>📁</button>
                      <button className="btn" disabled={busy === "rmloc"} onClick={() => doRemoveSave(loc.id)} title="Открепить от игры">✕</button>
                    </div>
                  </div>
                </div>
              ))}
            </div>
          </section>

          {/* Play sessions */}
          <section>
            <div className="mb-2 flex items-center justify-between">
              <h3 className="text-sm font-semibold uppercase tracking-wide text-muted">История запусков</h3>
              {view.game.totalPlaySeconds && view.game.totalPlaySeconds > 0 ? (
                <span className="text-xs text-muted">
                  всего: <span className="text-gray-200">{formatDuration(view.game.totalPlaySeconds)}</span>
                  {" · "}
                  {sessions.length} сессий
                </span>
              ) : null}
            </div>
            {sessions.length === 0 ? (
              <p className="text-sm text-muted">
                Нет данных. Трекер опрашивает запущенные процессы каждые 30 сек — после первого запуска игры (полный цикл: запустил → закрыл) сессия появится здесь.
              </p>
            ) : (
              <div className="grid gap-1.5">
                {sessions.map((s) => {
                  const active = !s.endedAt || s.endedAt === 0;
                  return (
                    <div key={s.id} className="card flex items-center gap-3 px-3 py-1.5 text-xs">
                      <span className={"chip " + (active ? "border-emerald-700/50 bg-emerald-900/50 text-emerald-300" : "")}>
                        {active ? "▶ играет сейчас" : s.source}
                      </span>
                      <span className="text-gray-200">{formatDate(s.startedAt)}</span>
                      {!active && (
                        <>
                          <span className="text-muted">→</span>
                          <span className="text-gray-300">{formatDate(s.endedAt)}</span>
                        </>
                      )}
                      <div className="flex-1" />
                      <span className="font-medium text-gray-100">
                        {active ? formatDuration(Math.floor(Date.now() / 1000 - s.startedAt)) + " (идёт)" : formatDuration(s.durationSeconds)}
                      </span>
                    </div>
                  );
                })}
              </div>
            )}
          </section>

          {/* Snapshots */}
          <section>
            <h3 className="mb-2 text-sm font-semibold uppercase tracking-wide text-muted">{t("drawer.snapshots")}</h3>
            {view.snapshots.length === 0 && <p className="text-sm text-muted">{t("drawer.noSnapshots")}</p>}
            <div className="grid gap-2">
              {view.snapshots.map((sn) => (
                <div key={sn.id} className="card flex items-center justify-between gap-3 px-3 py-2">
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-2 text-xs text-gray-300">
                      <span className="chip">{sn.trigger}</span>
                      <span>{formatDate(sn.createdAt)}</span>
                      <span>· {formatBytes(sn.compressedBytes)} / {formatBytes(sn.totalBytes)}</span>
                      <span>· {sn.fileCount} f</span>
                    </div>
                    <div className="mt-0.5 truncate text-xs text-muted" title={sn.archivePath}>{sn.archivePath}</div>
                  </div>
                  <div className="flex items-center gap-2">
                    <button className="btn" onClick={() => api.ShowItemInFolder(sn.archivePath).catch((e) => api.Toast("error", "Открыть папку: " + String(e)))}>📁</button>
                    <button className="btn" onClick={() => doRestore(sn.id)} disabled={busy === "restore"}>↺ {t("actions.restore")}</button>
                    <button className="btn" onClick={() => doDeleteSnap(sn.id)} disabled={busy === "delete"}>✕</button>
                  </div>
                </div>
              ))}
            </div>
          </section>
        </div>
      </div>

      {showPicker && (
        <ManifestPickerDialog
          initialQuery={view.game.name}
          onClose={() => setShowPicker(false)}
          onPicked={doPicked}
        />
      )}
      {showDeepScan && (
        <DeepScanDialog
          gameId={view.game.id}
          onClose={() => setShowDeepScan(false)}
          onChanged={() => { setShowDeepScan(false); onChanged(); }}
        />
      )}
    </div>
  );
}
