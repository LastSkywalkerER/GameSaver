import { useCallback, useEffect, useRef, useState } from "react";
import { api, coverUrl, formatBytes, formatDate, formatDuration, formatRelative, type GameView, type ManifestSearchResult, type PlaySession } from "../api";
import { SourceBadge } from "./SourceBadge";
import { useT } from "../i18n";
import { ManifestPickerDialog } from "./ManifestPicker";
import { DeepScanDialog } from "./DeepScanDialog";
import { confirmModal } from "./Modal";
import { useControllerButton, useControllerNav, useControllerScroll } from "../controller";
import { playBack, playMove, playSelect } from "../sound";

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
  // Save migration (between two of this game's save locations).
  const [migFrom, setMigFrom] = useState("");
  const [migTo, setMigTo] = useState("");
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

  // Drawer is the third gamepad surface (after carousel + power menu).
  // v0.10.1: d-pad / LS walks focusables inside the drawer with the
  // accent ring (same model as ShellSettingsPage), A activates the
  // focused control, right stick scrolls the body, B closes. The
  // ManifestPicker / DeepScan child dialogs shadow these handlers
  // when they're up.
  const scrollRef = useRef<HTMLDivElement | null>(null);

  // Walk focusable controls in DOM order — buttons, links, inputs.
  // Same trick as the settings page: re-collect on each move so newly
  // mounted rows (loaded sessions etc.) are reachable without a
  // MutationObserver.
  const collectFocusables = useCallback((): HTMLElement[] => {
    const root = scrollRef.current; if (!root) return [];
    const sel = [
      'button:not([disabled])',
      'input:not([disabled]):not([type="hidden"])',
      'select:not([disabled])',
      'textarea:not([disabled])',
      'a[href]',
      '[tabindex]:not([tabindex="-1"])',
    ].join(",");
    return Array.from(root.querySelectorAll<HTMLElement>(sel))
      .filter((el) => el.offsetParent !== null);
  }, []);

  // WebView2 won't paint :focus-visible on programmatic .focus(), so
  // we mark the active element with data-gs-focused="true" — see
  // style.css for the matching CSS rule.
  function setGamepadFocus(target: HTMLElement) {
    document.querySelectorAll<HTMLElement>('[data-gs-focused="true"]')
      .forEach((el) => el.removeAttribute("data-gs-focused"));
    target.setAttribute("data-gs-focused", "true");
    target.focus();
  }

  function moveDrawerFocus(delta: number) {
    const els = collectFocusables();
    if (!els.length) return;
    const cur = document.activeElement as HTMLElement | null;
    const i = cur ? els.indexOf(cur) : -1;
    const next = Math.max(0, Math.min(els.length - 1, (i < 0 ? 0 : i) + delta));
    if (next === i) return;
    const target = els[next];
    setGamepadFocus(target);
    target.scrollIntoView({ block: "nearest", behavior: "smooth" });
    playMove();
  }

  useControllerNav((dir) => {
    if (showPicker || showDeepScan) return;
    // ↑/↓ AND ←/→ all walk the focusable list — drawer rows are
    // single-column, so left/right would otherwise be silent.
    if (dir === "up" || dir === "left") moveDrawerFocus(-1);
    else if (dir === "down" || dir === "right") moveDrawerFocus(+1);
  });
  useControllerScroll(({ dy }) => {
    if (showPicker || showDeepScan) return;
    const el = scrollRef.current; if (!el) return;
    el.scrollBy({ top: dy * 18, behavior: "auto" });
  });
  useControllerButton((btn) => {
    if (showPicker || showDeepScan) return;
    if (btn === "a") {
      const el = document.activeElement as HTMLElement | null;
      if (!el) return;
      // Inputs/selects edit themselves — A clicking on a number input
      // would do nothing. Skip those; everything else (buttons etc.)
      // gets clicked.
      if (el.tagName === "INPUT" || el.tagName === "TEXTAREA" || el.tagName === "SELECT") return;
      playSelect();
      el.click();
    } else if (btn === "b" || btn === "back") {
      playBack(); onClose();
    }
  });

  // Autofocus the first control on open so a single ↓ press lands
  // somewhere useful. Delay one frame for the DOM to settle.
  useEffect(() => {
    const t = setTimeout(() => {
      const els = collectFocusables();
      if (els[0]) setGamepadFocus(els[0]);
    }, 50);
    return () => clearTimeout(t);
  }, [collectFocusables, view.game.id]);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (showPicker || showDeepScan) return;
      if (e.key === "Escape") onClose();
      else if (e.key === "PageDown") {
        e.preventDefault();
        scrollRef.current?.scrollBy({ top: +360, behavior: "smooth" });
      } else if (e.key === "PageUp") {
        e.preventDefault();
        scrollRef.current?.scrollBy({ top: -360, behavior: "smooth" });
      }
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
  async function doMigrate() {
    const from = migFrom || view.saveLocations[0]?.id;
    const to = migTo || view.saveLocations[1]?.id;
    if (!from || !to || from === to) {
      api.Toast("error", "Выбери разные источник и назначение");
      return;
    }
    const fromLoc = view.saveLocations.find((l) => l.id === from);
    const toLoc = view.saveLocations.find((l) => l.id === to);
    const ok = await confirmModal({
      title: "Перенести сейвы?",
      body: `Файлы из\n${fromLoc?.path}\nбудут скопированы в\n${toLoc?.path}\n(одноимённые перезапишутся). Текущее состояние назначения сначала забэкапится.`,
      confirmLabel: "Перенести",
    });
    if (!ok) return;
    setBusy("migrate");
    try {
      await api.MigrateSave(view.game.id, from, to);
      api.Toast("success", "Сейвы перенесены (создан preMigrate-бэкап).");
      onChanged();
    } catch (e) {
      api.Toast("error", "Перенос: " + String(e));
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
        ref={scrollRef}
        // overflow-x-hidden: drawer rows have long file paths (steamapps
        // / AppData) — without it the whole drawer scrolls sideways when
        // a path overflows, which read as the horizontal scrollbar in
        // the v0.9.x screenshots. Vertical scroll is the only intended
        // axis here.
        className="card relative h-full w-full max-w-3xl overflow-y-auto overflow-x-hidden rounded-none border-l border-border"
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

            {/* Migrate saves between this game's locations (e.g. pirate → Steam) */}
            {view.saveLocations.length >= 2 && (
              <div className="card mt-2 px-3 py-2">
                <div className="mb-1 text-xs font-semibold uppercase tracking-wide text-muted">Перенос сейвов между установками</div>
                <div className="flex flex-wrap items-center gap-2 text-sm">
                  <select
                    className="input min-w-0 flex-1"
                    value={migFrom || view.saveLocations[0].id}
                    onChange={(e) => setMigFrom(e.target.value)}
                    title="Источник"
                  >
                    {view.saveLocations.map((l) => (
                      <option key={l.id} value={l.id}>{l.sourceHint || l.kind}: …{l.path.slice(-40)}</option>
                    ))}
                  </select>
                  <span className="text-muted">→</span>
                  <select
                    className="input min-w-0 flex-1"
                    value={migTo || view.saveLocations[1].id}
                    onChange={(e) => setMigTo(e.target.value)}
                    title="Назначение"
                  >
                    {view.saveLocations.map((l) => (
                      <option key={l.id} value={l.id}>{l.sourceHint || l.kind}: …{l.path.slice(-40)}</option>
                    ))}
                  </select>
                  <button className="btn" disabled={busy === "migrate"} onClick={doMigrate}>
                    {busy === "migrate" ? "Переношу…" : "Перенести"}
                  </button>
                </div>
              </div>
            )}
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
                    // flex-wrap so on a narrow drawer the chip/date/end-date
                    // wraps to a second line instead of pushing the duration
                    // off the right edge.
                    <div key={s.id} className="card flex flex-wrap items-center gap-x-3 gap-y-1 px-3 py-1.5 text-xs">
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
                    <div className="flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-gray-300">
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
