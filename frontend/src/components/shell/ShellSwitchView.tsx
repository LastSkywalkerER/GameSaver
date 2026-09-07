// ShellSwitchView — the Nintendo Switch save browser for shell mode.
//
// Feature parity with the desktop <SwitchPage>: browse console saves per
// profile, back up a multi-selection, transfer both directions, and restore a
// backup onto a chosen profile. Same backend methods; only the interaction
// model differs.
//
// It is a separate component rather than a reuse of SwitchPage because that
// page is built from <select>, checkboxes and <Modal>, none of which a gamepad
// can drive. (ShellBackupsView made the same call for the same reason.)
//
// Controller bindings:
//   ↑ ↓ / LS      move          RS      fine scroll
//   LB / RB       switch tab (сейвы ↔ бэкапы)
//   A             pick / toggle selection
//   X             actions for the focused save
//   Y             back up the current selection
//   B / Esc       back one step, or close from the top level
//
// 🔴 Every step that writes to the console routes through the "confirm" step
// below. There is no path here that reaches TransferPCToSwitch or
// RestoreSwitchBackupToConsole without an explicit A-press on a confirmation
// screen, matching the desktop dialogs.

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  api,
  formatBytes,
  formatDate,
  type GameView,
  type SwitchBackup,
  type SwitchEntry,
  type SwitchLibrary,
  type SwitchStatus,
} from "../../api";
import { useControllerButton, useControllerNav, useControllerScroll } from "../../controller";
import { playBack, playMove, playSelect } from "../../sound";
import { allLocations, pcLocationsFor } from "../../switchMatch";

type Step =
  | { k: "list" }
  | { k: "actions"; title: string; profile: string }
  | { k: "pickLoc"; title: string; profile: string; dir: "toPC" | "toSwitch"; showAll: boolean }
  | { k: "restoreDest"; backup: SwitchBackup }
  | { k: "restoreProfile"; backup: SwitchBackup }
  | { k: "restoreLoc"; backup: SwitchBackup; showAll: boolean }
  | { k: "confirm"; text: string; danger: boolean; run: () => Promise<void>; back: Step };

type Item = { id: string; label: string; sub?: string; danger?: boolean; selected?: boolean };

const selKey = (title: string, profile: string) => JSON.stringify([title, profile]);

export function ShellSwitchView({
  status,
  games,
  onClose,
}: {
  status: SwitchStatus | null;
  games: GameView[];
  onClose: () => void;
}) {
  const connected = !!status?.connected;

  const [tab, setTab] = useState<"saves" | "backups">("saves");
  const [lib, setLib] = useState<SwitchLibrary | null>(null);
  const [backups, setBackups] = useState<SwitchBackup[]>([]);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [step, setStep] = useState<Step>({ k: "list" });
  const [idx, setIdx] = useState(0);
  const [busy, setBusy] = useState("");
  const [profiles, setProfiles] = useState<SwitchEntry[] | null>(null);
  const [note, setNote] = useState("");

  const listRef = useRef<HTMLDivElement | null>(null);
  const rowRefs = useRef<(HTMLElement | null)[]>([]);
  const lastMove = useRef(0);

  const refreshBackups = useCallback(async () => {
    try {
      setBackups(((await api.ListSwitchBackups()) ?? []) as SwitchBackup[]);
    } catch { /* disk read only; a failure here just leaves the list stale */ }
  }, []);

  useEffect(() => { refreshBackups(); }, [refreshBackups]);

  // Scan once on open when a console is present — in a controller-only context
  // there's no good reason to make the user press "scan" first.
  useEffect(() => {
    if (!connected || lib || busy) return;
    setBusy("scan");
    api.ScanSwitch()
      .then((l: any) => setLib(l as SwitchLibrary))
      .catch((e: any) => setNote(String(e?.message ?? e)))
      .finally(() => setBusy(""));
  }, [connected]);

  // ─── rows for the current step ───────────────────────────────────────────

  const saveRows = useMemo(() => {
    const out: { title: string; entry: SwitchEntry }[] = [];
    for (const t of lib?.titles ?? []) {
      for (const e of t.entries ?? []) {
        if (e.fileCount > 0) out.push({ title: t.name, entry: e });
      }
    }
    return out;
  }, [lib]);

  const backupCount = useMemo(() => {
    const m = new Map<string, number>();
    for (const b of backups) {
      const k = selKey(b.title, b.profile);
      m.set(k, (m.get(k) ?? 0) + 1);
    }
    return m;
  }, [backups]);

  const items: Item[] = useMemo(() => {
    switch (step.k) {
      case "list":
        if (tab === "saves") {
          return saveRows.map((r) => {
            const k = selKey(r.title, r.entry.name);
            const n = backupCount.get(k) ?? 0;
            return {
              id: k,
              label: `${r.title}`,
              sub: `${r.entry.name} · ${r.entry.fileCount} ф · ${formatBytes(r.entry.sizeBytes)}` +
                (n > 0 ? ` · ⛁${n}` : ""),
              selected: selected.has(k),
            };
          });
        }
        return backups.map((b) => ({
          id: b.archivePath,
          label: b.title,
          sub: `${b.profile} · ${formatDate(b.createdAt)} · ${b.fileCount} ф · ${formatBytes(b.sizeOnDisk)}`,
        }));

      case "actions":
        return [
          { id: "backup", label: "Забэкапить сейчас", sub: "Прочитать сейв с консоли в архив" },
          { id: "toPC", label: "Switch → ПК", sub: "Скопировать сейв консоли в папку игры на ПК" },
          { id: "toSwitch", label: "ПК → Switch", sub: "Записать сейв с ПК на консоль", danger: true },
        ];

      case "pickLoc": {
        const opts = step.showAll ? allLocations(games) : pcLocationsFor(games, step.title);
        const rows: Item[] = opts.map((o) => ({ id: o.id, label: o.label }));
        if (!step.showAll) {
          rows.push({
            id: "__all__",
            label: "Выбрать папку вручную…",
            sub: "Показать все игры — приложение не проверит, что это та же игра",
          });
        }
        return rows;
      }

      case "restoreDest":
        return [
          { id: "console", label: "Восстановить на консоль", sub: "Можно на другой профиль", danger: true },
          { id: "pc", label: "Распаковать в папку на ПК", sub: "Консоль не затрагивается" },
        ];

      case "restoreProfile":
        return (profiles ?? []).map((p) => ({
          id: p.name,
          label: p.name + (p.kind !== "profile" ? " (сейв-данные)" : ""),
          sub: `сейчас ${p.fileCount} ф · ${formatBytes(p.sizeBytes)}` +
            (p.name === step.backup.profile ? " · откуда снят бэкап" : ""),
          danger: p.name !== step.backup.profile,
        }));

      case "restoreLoc": {
        const opts = step.showAll ? allLocations(games) : pcLocationsFor(games, step.backup.title);
        const rows: Item[] = opts.map((o) => ({ id: o.id, label: o.label }));
        if (!step.showAll) {
          rows.push({ id: "__all__", label: "Выбрать папку вручную…", sub: "Показать все игры" });
        }
        return rows;
      }

      case "confirm":
        return [
          { id: "yes", label: "Да, выполнить", danger: step.danger },
          { id: "no", label: "Отмена" },
        ];
    }
  }, [step, tab, saveRows, backups, backupCount, selected, games, profiles]);

  // Keep the cursor inside the list whenever the step or its contents change.
  useEffect(() => { setIdx(0); }, [step.k, tab]);
  useEffect(() => {
    if (idx >= items.length) setIdx(Math.max(0, items.length - 1));
  }, [items.length, idx]);

  useEffect(() => {
    const el = rowRefs.current[idx];
    el?.scrollIntoView({ block: "nearest", behavior: "smooth" });
  }, [idx, step.k, tab]);

  // ─── actions ─────────────────────────────────────────────────────────────

  const goBack = useCallback(() => {
    playBack();
    setProfiles(null);
    switch (step.k) {
      case "list":
        onClose();
        break;
      case "confirm":
        setStep(step.back);
        break;
      case "actions":
      case "restoreDest":
        setStep({ k: "list" });
        break;
      case "pickLoc":
        setStep({ k: "actions", title: step.title, profile: step.profile });
        break;
      case "restoreProfile":
      case "restoreLoc":
        setStep({ k: "restoreDest", backup: step.backup });
        break;
    }
  }, [step, onClose]);

  async function runBusy(what: string, fn: () => Promise<void>) {
    setBusy(what);
    try {
      await fn();
    } catch (e: any) {
      api.Toast("error", String(e?.message ?? e));
    } finally {
      setBusy("");
    }
  }

  async function backupSelection(keys: string[]) {
    if (!keys.length) return;
    const sel = keys.map((k) => {
      const [title, profile] = JSON.parse(k) as [string, string];
      return { title, profile };
    });
    await runBusy("backup", async () => {
      const res = ((await api.BackupSwitchSelection(sel)) ?? []) as any[];
      const failed = res.filter((r) => r.error).length;
      const skipped = res.filter((r) => r.skipped && !r.error).length;
      api.Toast(failed ? "error" : "success",
        `Бэкап: создано ${res.length - failed - skipped}, без изменений ${skipped}` +
        (failed ? `, ошибок ${failed}` : ""));
      await refreshBackups();
    });
  }

  function pick(id: string) {
    switch (step.k) {
      case "list": {
        playSelect();
        if (tab === "saves") {
          // A toggles selection; X (handled below) opens the action menu.
          setSelected((prev) => {
            const next = new Set(prev);
            if (next.has(id)) next.delete(id); else next.add(id);
            return next;
          });
        } else {
          const b = backups.find((x) => x.archivePath === id);
          if (b) setStep({ k: "restoreDest", backup: b });
        }
        return;
      }

      case "actions": {
        playSelect();
        const { title, profile } = step;
        if (id === "backup") {
          void backupSelection([selKey(title, profile)]);
          setStep({ k: "list" });
          return;
        }
        setStep({ k: "pickLoc", title, profile, dir: id === "toPC" ? "toPC" : "toSwitch", showAll: false });
        return;
      }

      case "pickLoc": {
        playSelect();
        if (id === "__all__") { setStep({ ...step, showAll: true }); return; }
        const { title, profile, dir } = step;
        const locId = id;
        if (dir === "toPC") {
          setStep({
            k: "confirm",
            danger: true,
            text: `Скопировать сейв «${title} / ${profile}» с консоли в выбранную папку на ПК?\n` +
              "Текущее содержимое папки будет предварительно забэкаплено.",
            back: step,
            run: async () => {
              const r: any = await api.TransferSwitchToPC(title, profile, locId);
              api.Toast("success", `Перенесено файлов: ${r?.files ?? 0}`);
            },
          });
        } else {
          setStep({
            k: "confirm",
            danger: true,
            text: `Записать сейв с ПК на консоль в «${title} / ${profile}»?\n` +
              "Перед записью будет обязательно снят бэкап текущего сейва с консоли; " +
              "если он не удастся — перенос отменится.",
            back: step,
            run: async () => {
              await api.TransferPCToSwitch(locId, title, profile);
              api.Toast("success", "Сейв записан на консоль");
              await refreshBackups();
            },
          });
        }
        return;
      }

      case "restoreDest": {
        playSelect();
        if (id === "console") {
          setStep({ k: "restoreProfile", backup: step.backup });
          setProfiles(null);
          void api.GetSwitchTitleEntries(step.backup.title)
            .then((e: any) => setProfiles((e ?? []) as SwitchEntry[]))
            .catch((e: any) => { setNote(String(e?.message ?? e)); setProfiles([]); });
        } else {
          setStep({ k: "restoreLoc", backup: step.backup, showAll: false });
        }
        return;
      }

      case "restoreProfile": {
        playSelect();
        const b = step.backup;
        const target = id;
        setStep({
          k: "confirm",
          danger: true,
          text: `Восстановить бэкап «${b.title} / ${b.profile}» на консоль в профиль «${target}»?\n` +
            (target !== b.profile
              ? `Это ДРУГОЙ профиль — текущий сейв «${target}» будет перезаписан.\n`
              : "") +
            "Перед записью будет обязательно снят бэкап текущего сейва этого профиля.",
          back: step,
          run: async () => {
            await api.RestoreSwitchBackupToConsole(b.archivePath, b.title, target);
            api.Toast("success", `Восстановлено на консоль · профиль ${target}`);
            await refreshBackups();
          },
        });
        return;
      }

      case "restoreLoc": {
        playSelect();
        if (id === "__all__") { setStep({ ...step, showAll: true }); return; }
        const b = step.backup;
        const locId = id;
        setStep({
          k: "confirm",
          danger: true,
          text: `Распаковать бэкап «${b.title} / ${b.profile}» в выбранную папку на ПК?\n` +
            "Текущее содержимое папки будет предварительно забэкаплено. Консоль не затрагивается.",
          back: step,
          run: async () => {
            const r: any = await api.ExtractSwitchBackupToPC(b.archivePath, locId);
            api.Toast("success", `Распаковано файлов: ${r?.files ?? 0}`);
          },
        });
        return;
      }

      case "confirm": {
        if (id === "no") { goBack(); return; }
        playSelect();
        const { run } = step;
        setStep({ k: "list" });
        void runBusy("work", run);
        return;
      }
    }
  }

  // ─── input ───────────────────────────────────────────────────────────────

  function move(delta: number) {
    if (!items.length) return;
    const now = Date.now();
    if (now - lastMove.current < 100) return;
    lastMove.current = now;
    setIdx((i) => {
      const next = Math.max(0, Math.min(items.length - 1, i + delta));
      if (next !== i) playMove();
      return next;
    });
  }

  useControllerNav((dir) => {
    if (busy) return;
    if (dir === "up") move(-1);
    else if (dir === "down") move(+1);
  });
  useControllerScroll(({ dy }) => {
    listRef.current?.scrollBy({ top: dy * 18, behavior: "auto" });
  });
  useControllerButton((btn) => {
    if (busy) return;
    if (btn === "a") { const it = items[idx]; if (it) pick(it.id); }
    else if (btn === "b" || btn === "back") goBack();
    else if (btn === "x" && step.k === "list" && tab === "saves") {
      const r = saveRows[idx];
      if (r) { playSelect(); setStep({ k: "actions", title: r.title, profile: r.entry.name }); }
    } else if (btn === "y" && step.k === "list" && tab === "saves") {
      void backupSelection([...selected]);
    } else if ((btn === "lb" || btn === "rb") && step.k === "list") {
      playMove();
      setTab((t) => (t === "saves" ? "backups" : "saves"));
    }
  });

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (busy) return;
      if (e.key === "ArrowUp") { e.preventDefault(); move(-1); }
      else if (e.key === "ArrowDown") { e.preventDefault(); move(+1); }
      else if (e.key === "Enter") { e.preventDefault(); const it = items[idx]; if (it) pick(it.id); }
      else if (e.key === "Escape") { e.preventDefault(); goBack(); }
      else if (e.key === "Tab" && step.k === "list") {
        e.preventDefault();
        setTab((t) => (t === "saves" ? "backups" : "saves"));
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  });

  // ─── render ──────────────────────────────────────────────────────────────

  const heading =
    step.k === "list" ? (tab === "saves" ? "Switch · сейвы консоли" : "Switch · бэкапы")
      : step.k === "actions" ? `${step.title} · ${step.profile}`
      : step.k === "pickLoc" ? (step.dir === "toPC" ? "Куда положить на ПК" : "Что взять с ПК")
      : step.k === "restoreDest" ? `Восстановить · ${step.backup.title}`
      : step.k === "restoreProfile" ? "В какой профиль на консоли"
      : step.k === "restoreLoc" ? "В какую папку на ПК"
      : "Подтверждение";

  const hint =
    step.k === "list"
      ? "↑↓ выбор · A отметить · X действия · Y бэкап выбранного · LB/RB вкладка · B закрыть"
      : "↑↓ выбор · A подтвердить · B назад";

  const empty =
    step.k === "list" && tab === "saves" && !saveRows.length
      ? (connected ? (busy === "scan" ? "Читаю сейвы с консоли…" : "На консоли не найдено сейвов.")
                   : "Switch не подключён. Подключи консоль и запусти MTP-режим в DBI.")
      : step.k === "list" && tab === "backups" && !backups.length
      ? "Бэкапов с консоли пока нет."
      : step.k === "restoreProfile" && profiles === null
      ? "Читаю профили с консоли…"
      : items.length === 0
      ? "Подходящей игры на этом ПК не найдено."
      : "";

  return (
    <div
      className="fixed inset-0 z-[1500] flex flex-col items-center bg-black/85 p-8 backdrop-blur-md"
      onClick={onClose}
    >
      <div
        className="flex w-full max-w-6xl flex-1 flex-col gap-4 overflow-hidden"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center justify-between">
          <h1 className="text-3xl font-semibold text-gray-100">{heading}</h1>
          <div className="text-xs text-muted">{hint}</div>
        </div>

        <div className="flex items-center gap-3 text-xs">
          <span
            className={
              "flex items-center gap-2 rounded-md border px-2 py-1 " +
              (connected
                ? "border-emerald-700/50 bg-emerald-900/40 text-emerald-200"
                : "border-white/10 bg-white/5 text-muted")
            }
          >
            <span className={"h-2 w-2 rounded-full " + (connected ? "bg-emerald-400" : "bg-gray-500")} />
            {connected ? `подключён · ${status?.serial}` : "не подключён"}
          </span>
          {step.k === "list" && tab === "saves" && (
            <span className="text-muted">выбрано: {selected.size}</span>
          )}
          {busy && <span className="text-accent">работаю…</span>}
          {note && <span className="text-red-300">{note}</span>}
        </div>

        {step.k === "confirm" && (
          <div className="whitespace-pre-line rounded-2xl border border-amber-700/40 bg-amber-900/20 p-4 text-sm text-amber-100">
            {step.text}
          </div>
        )}

        {empty ? (
          <div className="rounded-2xl border-2 border-white/10 bg-white/5 p-8 text-center text-sm text-muted">
            {empty}
          </div>
        ) : (
          <div
            ref={listRef}
            className="flex-1 overflow-y-auto rounded-2xl border border-white/10 bg-white/5 p-3"
          >
            <div className="grid gap-1">
              {items.map((it, i) => (
                <div
                  key={it.id}
                  ref={(el) => { rowRefs.current[i] = el; }}
                  onMouseEnter={() => setIdx(i)}
                  onClick={(e) => { e.stopPropagation(); setIdx(i); pick(it.id); }}
                  className={
                    "flex cursor-pointer items-center gap-3 rounded-xl border-2 px-4 py-2.5 text-left text-sm transition " +
                    (i === idx
                      ? "border-accent bg-accent/20 shadow-[0_8px_25px_rgba(124,92,255,0.35)]"
                      : "border-white/10 bg-white/5 hover:bg-white/10")
                  }
                >
                  {it.selected !== undefined && (
                    <span
                      className={
                        "flex h-5 w-5 shrink-0 items-center justify-center rounded border text-[11px] " +
                        (it.selected ? "border-accent bg-accent text-black" : "border-white/30")
                      }
                    >
                      {it.selected ? "✓" : ""}
                    </span>
                  )}
                  <div className="min-w-0 flex-1">
                    <div className={"truncate font-medium " + (it.danger ? "text-amber-200" : "text-gray-100")}>
                      {it.label}
                    </div>
                    {it.sub && <div className="truncate text-xs text-muted">{it.sub}</div>}
                  </div>
                </div>
              ))}
            </div>
          </div>
        )}

        <div className="flex justify-end">
          <button className="btn" onClick={goBack}>
            {step.k === "list" ? "Закрыть (B / Esc)" : "Назад (B / Esc)"}
          </button>
        </div>
      </div>
    </div>
  );
}
