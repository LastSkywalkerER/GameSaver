import { useT } from "../i18n";

export function TopBar({
  scanning,
  onScan,
  onBackupAll,
  onOpenBackups,
  searchValue,
  onSearchChange,
  filter,
  onFilterChange,
}: {
  scanning: boolean;
  onScan: () => void;
  onBackupAll: () => void;
  onOpenBackups: () => void;
  searchValue: string;
  onSearchChange: (v: string) => void;
  filter: string;
  onFilterChange: (v: string) => void;
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
      <select className="input" value={filter} onChange={(e) => onFilterChange(e.target.value)}>
        <option value="all">{t("filter.all")}</option>
        <option value="withSaves">{t("filter.withSaves")}</option>
        <option value="withBackups">{t("filter.withBackups")}</option>
        <option value="withoutBackups">{t("filter.withoutBackups")}</option>
      </select>
    </header>
  );
}
