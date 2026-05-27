import { useEffect, useState } from "react";

type Dict = Record<string, string>;

const ru: Dict = {
  "app.title": "GameSaver",
  "nav.dashboard": "Дашборд",
  "nav.library": "Библиотека",
  "nav.backups": "Бэкапы",
  "nav.settings": "Настройки",

  "actions.scan": "Сканировать",
  "actions.scanning": "Сканирую…",
  "actions.reverseScan": "Поиск сейвов",
  "actions.reverseScanning": "Ищу сейвы…",
  "actions.backupAll": "Бэкап всех",
  "actions.refresh": "Обновить",
  "actions.openBackups": "Открыть папку бэкапов",
  "actions.play": "Запустить",
  "actions.backup": "Бэкап",
  "actions.restore": "Восстановить",
  "actions.delete": "Удалить",
  "actions.hide": "Скрыть",
  "actions.show": "Показать",
  "actions.pickFolder": "Выбрать папку",
  "actions.save": "Сохранить",
  "actions.close": "Закрыть",
  "actions.showInExplorer": "Показать в Проводнике",

  "tile.noSave": "сейв не найден",
  "tile.saves": "сейв(ы)",
  "tile.snaps": "снэпшот(ы)",
  "tile.installs": "установки",

  "filter.all": "Все",
  "filter.withSaves": "С сейвами",
  "filter.withBackups": "С бэкапами",
  "filter.withoutBackups": "Без бэкапов",
  "filter.search": "Поиск…",

  "drawer.installations": "Установки",
  "drawer.saveLocations": "Папки сейвов",
  "drawer.snapshots": "Снэпшоты",
  "drawer.noInstallations": "Установки не найдены",
  "drawer.noSaves": "Папки сейвов не найдены",
  "drawer.noSnapshots": "Снэпшотов пока нет",

  "settings.backupRoot": "Папка бэкапов",
  "settings.steamGridDbKey": "SteamGridDB API key",
  "settings.language": "Язык",
  "settings.theme": "Тема",
  "settings.retention": "Хранить последние N снэпшотов",
  "settings.watcher": "Автобэкап (watcher)",
  "settings.autostart": "Автозапуск в трее",

  "toast.scanDone": "Сканирование завершено",
  "toast.scanFailed": "Сканирование не удалось",
  "toast.backupStart": "Бэкап запущен…",
  "toast.backupDone": "Бэкап готов",
  "toast.restoreDone": "Восстановление готово",
  "toast.launchFailed": "Не удалось запустить",

  "scan.never": "Скан ещё не делался — нажмите «Сканировать»",
  "scan.empty": "Игры не найдены. Проверьте, что у вас стоит Steam/GOG/EGS/EA и т.п.",
};

const en: Dict = {
  "app.title": "GameSaver",
  "nav.dashboard": "Dashboard",
  "nav.library": "Library",
  "nav.backups": "Backups",
  "nav.settings": "Settings",

  "actions.scan": "Scan",
  "actions.scanning": "Scanning…",
  "actions.reverseScan": "Find saves",
  "actions.reverseScanning": "Finding saves…",
  "actions.backupAll": "Backup all",
  "actions.refresh": "Refresh",
  "actions.openBackups": "Open backups folder",
  "actions.play": "Play",
  "actions.backup": "Backup",
  "actions.restore": "Restore",
  "actions.delete": "Delete",
  "actions.hide": "Hide",
  "actions.show": "Show",
  "actions.pickFolder": "Pick folder",
  "actions.save": "Save",
  "actions.close": "Close",
  "actions.showInExplorer": "Show in Explorer",

  "tile.noSave": "no save found",
  "tile.saves": "save(s)",
  "tile.snaps": "snapshot(s)",
  "tile.installs": "installs",

  "filter.all": "All",
  "filter.withSaves": "With saves",
  "filter.withBackups": "With backups",
  "filter.withoutBackups": "Without backups",
  "filter.search": "Search…",

  "drawer.installations": "Installations",
  "drawer.saveLocations": "Save locations",
  "drawer.snapshots": "Snapshots",
  "drawer.noInstallations": "No installations",
  "drawer.noSaves": "No saves found",
  "drawer.noSnapshots": "No snapshots yet",

  "settings.backupRoot": "Backup folder",
  "settings.steamGridDbKey": "SteamGridDB API key",
  "settings.language": "Language",
  "settings.theme": "Theme",
  "settings.retention": "Keep last N snapshots",
  "settings.watcher": "Auto-backup (watcher)",
  "settings.autostart": "Autostart in tray",

  "toast.scanDone": "Scan complete",
  "toast.scanFailed": "Scan failed",
  "toast.backupStart": "Backup started…",
  "toast.backupDone": "Backup done",
  "toast.restoreDone": "Restore complete",
  "toast.launchFailed": "Failed to launch",

  "scan.never": "No scan yet — click Scan",
  "scan.empty": "No games found. Make sure Steam/GOG/EGS/EA etc. are installed.",
};

const DICTS: Record<string, Dict> = { ru, en };

// Default to the system language if we recognise it; otherwise English.
// This runs before any backend round-trip — once the backend's stored
// language arrives (only set if the user explicitly picked one), App.tsx
// calls setLanguage() to override.
function detectSystemLanguage(): string {
  try {
    const langs: string[] = [
      ...(navigator.languages ?? []),
      navigator.language ?? "",
    ];
    for (const l of langs) {
      const head = (l ?? "").toLowerCase().split("-")[0];
      if (head === "ru" || head === "en") return head;
    }
  } catch {}
  return "en";
}

let current: string = detectSystemLanguage();
const listeners = new Set<() => void>();

export function setLanguage(lang: string) {
  if (lang === "en" || lang === "ru") {
    current = lang;
    listeners.forEach((l) => l());
  }
}

export function getLanguage() {
  return current;
}

export function t(key: string): string {
  return DICTS[current][key] ?? DICTS["en"][key] ?? key;
}

export function useT() {
  const [, setVer] = useState(0);
  useEffect(() => {
    const fn = () => setVer((v) => v + 1);
    listeners.add(fn);
    return () => {
      listeners.delete(fn);
    };
  }, []);
  return t;
}
