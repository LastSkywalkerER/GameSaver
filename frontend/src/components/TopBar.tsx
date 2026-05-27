import { useT } from "../i18n";

export function TopBar({
  scanning,
  onScan,
  onBackupAll,
  onOpenBackups,
  searchValue,
  onSearchChange,
  sortBy,
  onSortByChange,
}: {
  scanning: boolean;
  onScan: () => void;
  onBackupAll: () => void;
  onOpenBackups: () => void;
  searchValue: string;
  onSearchChange: (v: string) => void;
  sortBy: string;
  onSortByChange: (v: string) => void;
}) {
  const t = useT();
  return (
    <header className="sticky top-0 z-20 flex items-center gap-3 border-b border-border bg-bg/80 px-5 py-3 backdrop-blur">
      <button
        className="btn btn-primary"
        disabled={scanning}
        onClick={onScan}
        title="Полный скан: лаунчеры + диски + матчинг сейвов + reverse-обход манифеста Ludusavi"
      >
        {scanning ? "↻ " + t("actions.scanning") : "↻ " + t("actions.scan")}
      </button>
      <button className="btn" onClick={onBackupAll}>⛁ {t("actions.backupAll")}</button>
      <button className="btn" onClick={onOpenBackups}>📁 {t("actions.openBackups")}</button>
      <div className="flex-1" />
      <input
        type="text"
        className="input w-64"
        placeholder={t("filter.search")}
        value={searchValue}
        onChange={(e) => onSearchChange(e.target.value)}
      />
      <select
        className="input"
        value={sortBy}
        onChange={(e) => onSortByChange(e.target.value)}
        title="Сортировка плиток"
      >
        <option value="name">По имени</option>
        <option value="recent">Недавно играл</option>
        <option value="playtime">Больше играл</option>
        <option value="installSize">Размер папки игры</option>
      </select>
    </header>
  );
}
