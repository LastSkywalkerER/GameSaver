// Nintendo Switch tab: browse the console's saves over MTP, back them up, and
// move saves between the console and this PC.
//
// The console is only reachable while it's plugged in and running DBI's MTP
// responder, so every device action is gated on the live switch:status event.
// Backups already on disk stay readable when it's unplugged — that's why the
// tab renders at all in the disconnected state instead of hiding itself.

import { useEffect, useMemo, useState } from "react";
import {
  api,
  formatBytes,
  formatDate,
  type GameView,
  type SwitchBackup,
  type SwitchEntry,
  type SwitchLibrary,
  type SwitchStatus,
  type SwitchTitle,
  type UploadPlan,
} from "../api";
import { Modal, confirmModal } from "../components/Modal";
import { allLocations, normTitle, pcLocationsFor } from "../switchMatch";

// A selection key must include the profile: 26 of the 74 titles on a typical
// console hold saves for more than one account, and backing a title up as a
// single unit would merge two people's saves into one archive.
//
// JSON rather than "title<sep>profile" because console titles contain spaces,
// unicode and punctuation ("LEGO® Звездные Войны™ Скайуокер. Сага"), so any
// printable separator risks splitting in the wrong place.
const selKey = (title: string, profile: string) => JSON.stringify([title, profile]);
const parseKey = (k: string) => {
  const [title, profile] = JSON.parse(k) as [string, string];
  return { title, profile };
};

// NoMatchNotice is shown when the console title has no counterpart on the PC.
// The manual override exists because the auto-match cannot bridge localised
// names (the console lists "Ведьмак 3 Дикая Охота", the PC copy is "The
// Witcher 3") — but it is opt-in and warned, not the default.
function NoMatchNotice({
  title,
  onShowAll,
}: {
  title: string;
  onShowAll: () => void;
}) {
  return (
    <div className="rounded-lg border border-border bg-card p-3 text-xs text-muted">
      <div className="mb-2 text-gray-300">
        На этом ПК не найдено игры «{title}» — переносить сейв не с чем и некуда.
      </div>
      <div className="mb-2">
        Если игра всё-таки установлена, но называется иначе (например, на консоли
        русское название, а на ПК английское), можно выбрать папку вручную.
      </div>
      <button className="btn !px-2 !py-0.5 !text-xs" onClick={onShowAll}>
        Всё равно выбрать папку вручную
      </button>
    </div>
  );
}

// ManualPickWarning sits above a manually-chosen destination.
function ManualPickWarning() {
  return (
    <div className="rounded-lg border border-amber-700/40 bg-amber-900/20 p-2 text-xs text-amber-200">
      Ручной выбор: приложение не проверяет, что это та же игра. Сейв от другой
      игры консоль не прочитает.
    </div>
  );
}

export function SwitchPage({
  status,
  games,
  progress,
}: {
  status: SwitchStatus | null;
  games: GameView[];
  progress: { phase: string; done: number; total: number; title: string } | null;
}) {
  const [lib, setLib] = useState<SwitchLibrary | null>(null);
  const [backups, setBackups] = useState<SwitchBackup[]>([]);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [busy, setBusy] = useState<string>("");
  const [query, setQuery] = useState("");
  const [onlyWithSaves, setOnlyWithSaves] = useState(true);
  const [transfer, setTransfer] = useState<{ title: string; profile: string } | null>(null);
  const [restoring, setRestoring] = useState<SwitchBackup | null>(null);

  const connected = !!status?.connected;

  async function refreshBackups() {
    try {
      setBackups(((await api.ListSwitchBackups()) ?? []) as SwitchBackup[]);
    } catch (e) {
      console.error(e);
    }
  }
  useEffect(() => { refreshBackups(); }, []);

  async function onScan() {
    setBusy("scan");
    try {
      const l = (await api.ScanSwitch()) as SwitchLibrary;
      setLib(l);
      setSelected(new Set());
      api.Toast("success", `Найдено игр на консоли: ${l?.titles?.length ?? 0}`);
    } catch (e: any) {
      api.Toast("error", String(e?.message ?? e));
    } finally {
      setBusy("");
    }
  }

  async function onBackup() {
    if (selected.size === 0) return;
    setBusy("backup");
    try {
      const sel = [...selected].map(parseKey);
      const res = ((await api.BackupSwitchSelection(sel)) ?? []) as any[];
      const failed = res.filter((r) => r.error);
      const skipped = res.filter((r) => r.skipped && !r.error);
      const made = res.length - failed.length - skipped.length;
      api.Toast(
        failed.length ? "error" : "success",
        `Бэкап: создано ${made}, без изменений ${skipped.length}` +
          (failed.length ? `, ошибок ${failed.length}` : ""),
      );
      await refreshBackups();
    } catch (e: any) {
      api.Toast("error", String(e?.message ?? e));
    } finally {
      setBusy("");
    }
  }

  const titles = useMemo(() => {
    let ts = (lib?.titles ?? []) as SwitchTitle[];
    if (onlyWithSaves) ts = ts.filter((t) => (t.entries ?? []).some((e) => e.fileCount > 0));
    const q = query.trim().toLowerCase();
    if (q) ts = ts.filter((t) => t.name.toLowerCase().includes(q));
    return ts;
  }, [lib, query, onlyWithSaves]);

  // How many backups exist per (title, profile) — shown as a chip so the user
  // can tell at a glance what is already protected.
  const backupCount = useMemo(() => {
    const m = new Map<string, number>();
    for (const b of backups) {
      const k = selKey(b.title, b.profile);
      m.set(k, (m.get(k) ?? 0) + 1);
    }
    return m;
  }, [backups]);

  function toggle(title: string, profile: string) {
    setSelected((prev) => {
      const next = new Set(prev);
      const k = selKey(title, profile);
      if (next.has(k)) next.delete(k);
      else next.add(k);
      return next;
    });
  }

  // Bulk helpers operate on what's currently filtered, not the whole library —
  // otherwise "select all" after a search would quietly select hidden rows.
  function selectAllProfiles() {
    const next = new Set(selected);
    for (const t of titles) {
      for (const e of t.entries ?? []) {
        if (e.kind === "profile" && e.fileCount > 0) next.add(selKey(t.name, e.name));
      }
    }
    setSelected(next);
  }

  function selectProfileNamed(name: string) {
    const next = new Set(selected);
    for (const t of titles) {
      for (const e of t.entries ?? []) {
        if (e.name === name && e.fileCount > 0) next.add(selKey(t.name, e.name));
      }
    }
    setSelected(next);
  }

  // Normalised names of PC games that have a save folder, so each row can tell
  // whether a transfer has a counterpart here. Not a hard gate on the button:
  // localised titles ("Ведьмак 3" on the console, "The Witcher 3" on the PC)
  // legitimately fail to match, and hiding the button would leave that case
  // with no way in at all.
  const pcTitles = useMemo(() => {
    const s = new Set<string>();
    for (const g of games) if ((g.saveLocations ?? []).length > 0) s.add(normTitle(g.game.name));
    return s;
  }, [games]);

  // Distinct console account names, for the per-profile bulk buttons.
  const profileNames = useMemo(() => {
    const s = new Set<string>();
    for (const t of lib?.titles ?? []) {
      for (const e of t.entries ?? []) if (e.kind === "profile") s.add(e.name);
    }
    return [...s].sort();
  }, [lib]);

  return (
    <div className="flex h-full flex-col">
      {/* ── toolbar ───────────────────────────────────────────────── */}
      <div className="flex flex-wrap items-center gap-2 border-b border-border bg-panel px-5 py-3">
        <span
          className={
            "flex items-center gap-2 rounded-md border px-2 py-1 text-xs " +
            (connected
              ? "border-emerald-700/50 bg-emerald-900/40 text-emerald-200"
              : "border-border bg-card text-muted")
          }
          title={status?.error || (connected ? status?.serial : "Подключи консоль и запусти MTP-режим в DBI")}
        >
          <span className={"h-2 w-2 rounded-full " + (connected ? "bg-emerald-400" : "bg-gray-500")} />
          {connected ? `Switch подключён · ${status?.serial}` : "Switch не подключён"}
        </span>

        <button className="btn" onClick={onScan} disabled={!connected || !!busy}>
          {busy === "scan" ? "Сканирую…" : "Сканировать консоль"}
        </button>
        <button className="btn btn-primary" onClick={onBackup} disabled={!connected || !!busy || selected.size === 0}>
          {busy === "backup" ? "Бэкаплю…" : `Забэкапить выбранное (${selected.size})`}
        </button>

        <div className="flex-1" />
        <input
          className="input w-56"
          placeholder="Поиск по играм…"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
        />
        <label className="flex items-center gap-1.5 text-xs text-muted" title="Скрыть игры, у которых на консоли нет файлов сейвов">
          <input type="checkbox" checked={onlyWithSaves} onChange={(e) => setOnlyWithSaves(e.target.checked)} />
          только с сейвами
        </label>
      </div>

      {/* ── bulk selection helpers ────────────────────────────────── */}
      {lib && (
        <div className="flex flex-wrap items-center gap-2 border-b border-border px-5 py-2 text-xs text-muted">
          <span>Выделить:</span>
          <button className="btn !px-2 !py-0.5 !text-xs" onClick={selectAllProfiles}>все профили</button>
          {profileNames.map((p) => (
            <button key={p} className="btn !px-2 !py-0.5 !text-xs" onClick={() => selectProfileNamed(p)}>
              {p}
            </button>
          ))}
          <button className="btn !px-2 !py-0.5 !text-xs" onClick={() => setSelected(new Set())}>снять всё</button>
          <span className="ml-2 opacity-70">
            {titles.length} игр показано · сканировано {formatDate(lib.scannedAt)}
          </span>
        </div>
      )}

      {progress && progress.total > 0 && (
        <div className="border-b border-border bg-panel/80 px-5 py-1 text-xs text-muted">
          ⏳ {progress.phase}: {progress.done}/{progress.total} {progress.title}
        </div>
      )}

      {/* ── body ──────────────────────────────────────────────────── */}
      <div className="flex-1 overflow-y-auto">
        {!lib && (
          <div className="p-10 text-center text-muted">
            {connected
              ? "Нажми «Сканировать консоль», чтобы прочитать сейвы со Switch."
              : "Подключи Switch по USB и запусти в DBI режим MTP. Уже сделанные бэкапы видны ниже и без консоли."}
          </div>
        )}

        {lib && (
          <div className="p-5">
            <table className="w-full text-sm">
              <thead className="text-left text-muted">
                <tr>
                  <th className="px-2 py-2">Игра</th>
                  <th className="px-2 py-2">Профили и сейв-данные</th>
                </tr>
              </thead>
              <tbody>
                {titles.map((t) => {
                  const hasPC = pcTitles.has(normTitle(t.name));
                  return (
                  <tr key={t.objectId} className="border-t border-border align-top">
                    <td className="px-2 py-2">
                      <div className="font-medium text-gray-100">{t.name}</div>
                      {!t.installed && (
                        <div className="text-[11px] text-amber-300/80" title="Игра удалена с консоли, но её сейв сохранился">
                          игра не установлена
                        </div>
                      )}
                    </td>
                    <td className="px-2 py-2">
                      <div className="flex flex-wrap gap-1.5">
                        {(t.entries ?? []).map((e) => {
                          const k = selKey(t.name, e.name);
                          const on = selected.has(k);
                          const empty = e.fileCount === 0;
                          const nBk = backupCount.get(k) ?? 0;
                          return (
                            <span
                              key={e.objectId}
                              className={
                                "flex items-center gap-1.5 rounded-md border px-2 py-1 text-xs " +
                                (on ? "border-accent bg-accent/15 text-accent" : "border-border bg-card text-gray-300") +
                                (empty ? " opacity-40" : "")
                              }
                            >
                              <input
                                type="checkbox"
                                checked={on}
                                disabled={empty}
                                onChange={() => toggle(t.name, e.name)}
                              />
                              <span className={e.kind === "profile" ? "font-medium" : "italic opacity-80"}>
                                {e.name}
                              </span>
                              <span className="opacity-70">
                                {e.fileCount} ф · {formatBytes(e.sizeBytes)}
                              </span>
                              {nBk > 0 && (
                                <span className="rounded bg-emerald-900/50 px-1 text-[10px] text-emerald-300" title={`${nBk} бэкапов`}>
                                  ⛁{nBk}
                                </span>
                              )}
                              {!empty && connected && (
                                <button
                                  className={
                                    "rounded border px-1.5 py-0.5 text-[10px] transition " +
                                    (hasPC
                                      ? "border-border bg-panel text-gray-300 hover:border-accent hover:text-accent"
                                      : "border-transparent text-gray-500 hover:text-gray-300")
                                  }
                                  title={
                                    hasPC
                                      ? "Скопировать этот сейв на ПК или подсунуть вместо него сейв с ПК"
                                      : "На ПК нет игры с таким названием — перенос возможен только через ручной выбор папки"
                                  }
                                  onClick={() => setTransfer({ title: t.name, profile: e.name })}
                                >
                                  перенос ⇄
                                </button>
                              )}
                            </span>
                          );
                        })}
                      </div>
                    </td>
                  </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}

        {/* ── existing console backups ───────────────────────────── */}
        <div className="border-t border-border p-5">
          <div className="mb-2 text-sm font-medium text-gray-200">
            Бэкапы с консоли ({backups.length})
          </div>
          {backups.length === 0 ? (
            <div className="text-xs text-muted">Пока ничего не забэкаплено.</div>
          ) : (
            <table className="w-full text-sm">
              <thead className="text-left text-muted">
                <tr>
                  <th className="px-2 py-2">Игра</th>
                  <th className="px-2 py-2">Профиль</th>
                  <th className="px-2 py-2">Когда</th>
                  <th className="px-2 py-2 text-right">Файлов</th>
                  <th className="px-2 py-2 text-right">Размер</th>
                  <th className="px-2 py-2">Восстановить</th>
                  <th className="px-2 py-2">Архив</th>
                </tr>
              </thead>
              <tbody>
                {backups.map((b) => (
                  <tr key={b.archivePath} className="border-t border-border">
                    <td className="px-2 py-2">
                      <span className="mr-2 rounded bg-red-900/40 px-1.5 py-0.5 text-[10px] text-red-200">Switch</span>
                      <span className="text-gray-100">{b.title}</span>
                    </td>
                    <td className="px-2 py-2 text-gray-300">{b.profile}</td>
                    <td className="px-2 py-2 text-gray-300">{formatDate(b.createdAt)}</td>
                    <td className="px-2 py-2 text-right text-gray-300">{b.fileCount}</td>
                    <td className="px-2 py-2 text-right text-gray-300">{formatBytes(b.sizeOnDisk)}</td>
                    <td className="px-2 py-2">
                      <button
                        className="btn !px-2 !py-0.5 !text-xs"
                        title="Восстановить этот бэкап — на консоль (можно на другой профиль) или в папку на ПК"
                        onClick={() => setRestoring(b)}
                      >
                        Восстановить…
                      </button>
                    </td>
                    <td className="px-2 py-2 truncate text-xs text-muted" title={b.archivePath}>
                      {b.archivePath}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
      </div>

      {transfer && (
        <TransferModal
          title={transfer.title}
          profile={transfer.profile}
          games={games}
          onClose={() => setTransfer(null)}
          onDone={refreshBackups}
        />
      )}

      {restoring && (
        <RestoreModal
          backup={restoring}
          games={games}
          connected={connected}
          onClose={() => setRestoring(null)}
          onDone={refreshBackups}
        />
      )}
    </div>
  );
}

// ─── restore dialog ────────────────────────────────────────────────────────

// Restoring a console backup has two useful destinations, and the console one
// needs a profile picker: the whole reason to restore is often to move a save
// from one account to another, so the destination profile is not necessarily
// the one the backup came from.
function RestoreModal({
  backup,
  games,
  connected,
  onClose,
  onDone,
}: {
  backup: SwitchBackup;
  games: GameView[];
  connected: boolean;
  onClose: () => void;
  onDone: () => void;
}) {
  const [dest, setDest] = useState<"console" | "pc">(connected ? "console" : "pc");
  const [entries, setEntries] = useState<SwitchEntry[] | null>(null);
  const [profile, setProfile] = useState(backup.profile);
  const [locationId, setLocationId] = useState("");
  const [busy, setBusy] = useState(false);
  const [loadErr, setLoadErr] = useState("");

  // Which profiles actually exist for this title on the console. We can't
  // invent one — DBI only exposes accounts that already hold save data for
  // the title — so the picker offers exactly those.
  useEffect(() => {
    if (dest !== "console" || !connected) return;
    setEntries(null);
    setLoadErr("");
    api
      .GetSwitchTitleEntries(backup.title)
      .then((e: any) => setEntries((e ?? []) as SwitchEntry[]))
      .catch((e: any) => setLoadErr(String(e?.message ?? e)));
  }, [dest, connected, backup.title]);

  // Same rule as the transfer dialog: a console save only belongs in the PC
  // folder of the same game.
  const [showAllPC, setShowAllPC] = useState(false);
  const matchedPC = useMemo(() => pcLocationsFor(games, backup.title), [games, backup.title]);
  const pcOptions = showAllPC ? allLocations(games) : matchedPC;

  useEffect(() => {
    if (!locationId && matchedPC.length === 1) setLocationId(matchedPC[0].id);
  }, [matchedPC, locationId]);

  async function run() {
    if (dest === "pc") {
      if (!locationId) return;
      const ok = await confirmModal({
        title: "Восстановить бэкап в папку на ПК?",
        body:
          `Файлы из бэкапа «${backup.title} / ${backup.profile}» будут распакованы в выбранную папку.\n\n` +
          "Текущее содержимое папки будет автоматически забэкаплено перед перезаписью. Консоль не затрагивается.",
        confirmLabel: "Восстановить",
        variant: "danger",
      });
      if (!ok) return;
      setBusy(true);
      try {
        const r: any = await api.ExtractSwitchBackupToPC(backup.archivePath, locationId);
        api.Toast("success", `Распаковано файлов: ${r?.files ?? 0}`);
        onClose();
      } catch (e: any) {
        api.Toast("error", String(e?.message ?? e));
      } finally {
        setBusy(false);
      }
      return;
    }

    const crossProfile = profile !== backup.profile;
    const ok = await confirmModal({
      title: "Восстановить бэкап на консоль?",
      body:
        `Бэкап «${backup.title} / ${backup.profile}» от ${formatDate(backup.createdAt)} ` +
        `будет записан на консоль в профиль «${profile}».\n\n` +
        (crossProfile
          ? `⚠ Это ДРУГОЙ профиль, не тот, с которого снят бэкап (${backup.profile}). ` +
            `Текущий сейв профиля «${profile}» будет перезаписан.\n\n`
          : "") +
        "Перед записью приложение обязательно снимет бэкап текущего сейва этого профиля — " +
        "если бэкап не удастся, восстановление будет отменено.",
      confirmLabel: "Восстановить на консоль",
      variant: "danger",
    });
    if (!ok) return;
    setBusy(true);
    try {
      await api.RestoreSwitchBackupToConsole(backup.archivePath, backup.title, profile);
      api.Toast("success", `Сейв восстановлен на консоль · профиль ${profile}`);
      onDone();
      onClose();
    } catch (e: any) {
      api.Toast("error", String(e?.message ?? e));
    } finally {
      setBusy(false);
    }
  }

  const canRun = dest === "pc" ? !!locationId : connected && !!profile && !loadErr;

  return (
    <Modal
      open
      size="2xl"
      title={`Восстановление · ${backup.title} / ${backup.profile}`}
      onClose={onClose}
      footer={
        <>
          <button className="btn" onClick={onClose}>Отмена</button>
          <button className="btn btn-primary" onClick={run} disabled={!canRun || busy}>
            {busy ? "Работаю…" : dest === "console" ? "Восстановить на консоль" : "Распаковать на ПК"}
          </button>
        </>
      }
    >
      <div className="space-y-4">
        <div className="text-xs text-muted">
          Снят {formatDate(backup.createdAt)} · {backup.fileCount} файлов · {formatBytes(backup.totalBytes)}
          {backup.serial ? ` · консоль ${backup.serial}` : ""}
        </div>

        <div className="flex gap-2">
          <button
            className={"btn flex-1 " + (dest === "console" ? "btn-primary" : "")}
            onClick={() => setDest("console")}
            disabled={!connected}
            title={connected ? undefined : "Нужна подключённая консоль"}
          >
            На консоль
          </button>
          <button
            className={"btn flex-1 " + (dest === "pc" ? "btn-primary" : "")}
            onClick={() => setDest("pc")}
          >
            В папку на ПК
          </button>
        </div>

        {dest === "console" && (
          <div>
            <div className="mb-1 text-xs text-muted">В какой профиль на консоли</div>
            {loadErr && <div className="text-xs text-red-300">{loadErr}</div>}
            {!entries && !loadErr && <div className="text-xs text-muted">Читаю профили с консоли…</div>}
            {entries && (
              <select className="input w-full" value={profile} onChange={(e) => setProfile(e.target.value)}>
                {entries.map((e) => (
                  <option key={e.objectId} value={e.name}>
                    {e.name}
                    {e.kind !== "profile" ? " (сейв-данные)" : ""} — сейчас {e.fileCount} ф · {formatBytes(e.sizeBytes)}
                    {e.name === backup.profile ? " · откуда снят бэкап" : ""}
                  </option>
                ))}
              </select>
            )}
            {entries && profile !== backup.profile && (
              <div className="mt-2 rounded-lg border border-amber-700/40 bg-amber-900/20 p-2 text-xs text-amber-200">
                Восстановление на другой профиль: сейв аккаунта «{backup.profile}» будет записан
                поверх сейва «{profile}».
              </div>
            )}
          </div>
        )}

        {dest === "pc" && (
          pcOptions.length === 0 ? (
            <NoMatchNotice title={backup.title} onShowAll={() => setShowAllPC(true)} />
          ) : (
            <div className="space-y-2">
              <div className="text-xs text-muted">
                В какую папку сейва на ПК{!showAllPC && " · только папки этой же игры"}
              </div>
              {showAllPC && <ManualPickWarning />}
              <select className="input w-full" value={locationId} onChange={(e) => setLocationId(e.target.value)}>
                <option value="">— выбери папку сейва —</option>
                {pcOptions.map((o) => (
                  <option key={o.id} value={o.id}>{o.label}</option>
                ))}
              </select>
              <div className="text-xs text-muted">Консоль при этом не затрагивается.</div>
            </div>
          )
        )}
      </div>
    </Modal>
  );
}

// ─── transfer dialog ───────────────────────────────────────────────────────

function TransferModal({
  title,
  profile,
  games,
  onClose,
  onDone,
}: {
  title: string;
  profile: string;
  games: GameView[];
  onClose: () => void;
  onDone: () => void;
}) {
  const [dir, setDir] = useState<"toPC" | "toSwitch">("toPC");
  const [locationId, setLocationId] = useState("");
  const [plan, setPlan] = useState<UploadPlan | null>(null);
  const [busy, setBusy] = useState(false);

  const [showAll, setShowAll] = useState(false);
  const matched = useMemo(() => pcLocationsFor(games, title), [games, title]);
  const options = showAll ? allLocations(games) : matched;

  // With exactly one matching folder there is nothing to choose — pick it so
  // the common case is a single click.
  useEffect(() => {
    if (!locationId && matched.length === 1) setLocationId(matched[0].id);
  }, [matched, locationId]);

  async function onPreview() {
    setBusy(true);
    try {
      setPlan((await api.PreviewSwitchUpload(locationId, title, profile)) as UploadPlan);
    } catch (e: any) {
      api.Toast("error", String(e?.message ?? e));
    } finally {
      setBusy(false);
    }
  }

  async function onRun() {
    if (!locationId) return;
    if (dir === "toPC") {
      const ok = await confirmModal({
        title: "Перенести сейв со Switch на ПК?",
        body:
          `Файлы сейва «${title} / ${profile}» будут скопированы в выбранную папку на ПК.\n\n` +
          "Текущее состояние сейва на ПК будет автоматически забэкаплено перед перезаписью.",
        confirmLabel: "Перенести",
        variant: "danger",
      });
      if (!ok) return;
      setBusy(true);
      try {
        const r: any = await api.TransferSwitchToPC(title, profile, locationId);
        api.Toast("success", `Перенесено файлов: ${r?.files ?? 0}`);
        onClose();
      } catch (e: any) {
        api.Toast("error", String(e?.message ?? e));
      } finally {
        setBusy(false);
      }
      return;
    }

    // PC → Switch is the only direction that modifies the console.
    const ok = await confirmModal({
      title: "Записать сейв с ПК на консоль?",
      body:
        `На консоли будет изменён сейв «${title} / ${profile}».\n\n` +
        (plan
          ? `Заменится файлов: ${plan.replaced}, добавится: ${plan.newFiles}. ` +
            `Останется нетронутыми: ${plan.untouched?.length ?? 0}.\n\n`
          : "") +
        "Перед записью приложение обязательно снимет бэкап текущего сейва с консоли — " +
        "если бэкап не удастся, перенос будет отменён.",
      confirmLabel: "Записать на консоль",
      variant: "danger",
    });
    if (!ok) return;
    setBusy(true);
    try {
      await api.TransferPCToSwitch(locationId, title, profile);
      api.Toast("success", "Сейв записан на консоль");
      onDone();
      onClose();
    } catch (e: any) {
      api.Toast("error", String(e?.message ?? e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Modal
      open
      size="2xl"
      title={`Перенос сейва · ${title} / ${profile}`}
      onClose={onClose}
      footer={
        <>
          <button className="btn" onClick={onClose}>Отмена</button>
          {dir === "toSwitch" && (
            <button className="btn" onClick={onPreview} disabled={!locationId || busy}>
              Показать план
            </button>
          )}
          <button className="btn btn-primary" onClick={onRun} disabled={!locationId || busy}>
            {busy ? "Работаю…" : dir === "toPC" ? "Switch → ПК" : "ПК → Switch"}
          </button>
        </>
      }
    >
      <div className="space-y-4">
        <div className="flex gap-2">
          <button
            className={"btn flex-1 " + (dir === "toPC" ? "btn-primary" : "")}
            onClick={() => { setDir("toPC"); setPlan(null); }}
          >
            Switch → ПК
          </button>
          <button
            className={"btn flex-1 " + (dir === "toSwitch" ? "btn-primary" : "")}
            onClick={() => { setDir("toSwitch"); setPlan(null); }}
          >
            ПК → Switch
          </button>
        </div>

        {options.length === 0 ? (
          <NoMatchNotice title={title} onShowAll={() => setShowAll(true)} />
        ) : (
          <div className="space-y-2">
            <div className="text-xs text-muted">
              {dir === "toPC" ? "Куда положить на ПК" : "Что взять с ПК"}
              {!showAll && " · только папки этой же игры"}
            </div>
            {showAll && <ManualPickWarning />}
            <select
              className="input w-full"
              value={locationId}
              onChange={(e) => { setLocationId(e.target.value); setPlan(null); }}
            >
              <option value="">— выбери папку сейва —</option>
              {options.map((o) => (
                <option key={o.id} value={o.id}>{o.label}</option>
              ))}
            </select>
          </div>
        )}

        {dir === "toSwitch" && (
          <div className="rounded-lg border border-amber-700/40 bg-amber-900/20 p-3 text-xs text-amber-200">
            Единственное действие в приложении, которое пишет на консоль. Перед записью
            автоматически снимается бэкап текущего сейва со Switch; если он не удастся —
            перенос не состоится.
          </div>
        )}

        {plan && (
          <div className="rounded-lg border border-border bg-card p-3">
            <div className="mb-2 text-xs text-muted">
              План (ничего ещё не записано): заменить {plan.replaced}, добавить {plan.newFiles},
              всего {formatBytes(plan.totalBytes)} · не тронем {plan.untouched?.length ?? 0} файлов на консоли
            </div>
            <div className="max-h-48 overflow-y-auto font-mono text-[11px]">
              {(plan.writes ?? []).map((w) => (
                <div key={w.rel} className="flex justify-between gap-3">
                  <span className={w.replaces ? "text-amber-300" : "text-emerald-300"}>
                    {w.replaces ? "заменить" : "добавить"} {w.rel}
                  </span>
                  <span className="text-muted">{formatBytes(w.size)}</span>
                </div>
              ))}
            </div>
          </div>
        )}
      </div>
    </Modal>
  );
}
