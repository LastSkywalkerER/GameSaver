// Per-user preferences for which chips appear on a game tile. Stored in
// localStorage and broadcast via a tiny subscriber pattern so any component
// using `useTilePrefs()` re-renders when the user flips a checkbox.

import { useEffect, useState } from "react";

export type TilePrefs = {
  showSources: boolean;       // bottom-strip Steam/GOG/EA/... badges
  showSaveCount: boolean;     // top-right "✓ N" chip
  showBackupCount: boolean;   // top-right "⛁ N" chip
  showLastPlayed: boolean;    // top-left "▶ <relative>" chip
  showPlaytime: boolean;      // top-left "⏱ <duration>" chip
  showLastBackup: boolean;    // top-left "⛁ <relative>" chip
  showInstallSize: boolean;   // top-left "💾 H: 87 GB" per-drive chips
  showNewBadges: boolean;     // "NEW" / "только сейв" status pills
};

const DEFAULTS: TilePrefs = {
  showSources: true,
  showSaveCount: true,
  showBackupCount: true,
  showLastPlayed: true,
  showPlaytime: true,
  showLastBackup: true,
  showInstallSize: true,
  showNewBadges: true,
};

const STORAGE_KEY = "gs:tilePrefs";

function load(): TilePrefs {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (!raw) return { ...DEFAULTS };
    const parsed = JSON.parse(raw) as Partial<TilePrefs>;
    return { ...DEFAULTS, ...parsed };
  } catch {
    return { ...DEFAULTS };
  }
}

let current: TilePrefs = load();
const subscribers = new Set<() => void>();

function persist() {
  try { localStorage.setItem(STORAGE_KEY, JSON.stringify(current)); } catch {}
  subscribers.forEach((fn) => fn());
}

export function getTilePrefs(): TilePrefs {
  return current;
}

export function setTilePref<K extends keyof TilePrefs>(key: K, value: TilePrefs[K]) {
  current = { ...current, [key]: value };
  persist();
}

export function useTilePrefs(): TilePrefs {
  const [, setVer] = useState(0);
  useEffect(() => {
    const fn = () => setVer((v) => v + 1);
    subscribers.add(fn);
    return () => { subscribers.delete(fn); };
  }, []);
  return current;
}

// Labels used by the Settings page so we don't duplicate the list.
export const TILE_PREF_LABELS: Record<keyof TilePrefs, string> = {
  showSources: "Источники (Steam/GOG/EA/…)",
  showSaveCount: "Счётчик сейвов (✓ N)",
  showBackupCount: "Счётчик бэкапов (⛁ N)",
  showLastPlayed: "Последний запуск",
  showPlaytime: "Время в игре",
  showLastBackup: "Последний бэкап",
  showInstallSize: "Размер папки игры (диск, ГБ)",
  showNewBadges: "Бейджи NEW / «только сейв»",
};
