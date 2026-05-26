import type { GameView } from "../api";
import { GameTile } from "../components/GameTile";
import { useT } from "../i18n";

export function DashboardPage({
  games,
  onOpen,
  query,
  filter,
  scanned,
}: {
  games: GameView[];
  onOpen: (g: GameView) => void;
  query: string;
  filter: string;
  scanned: boolean;
}) {
  const t = useT();
  const filtered = games.filter((g) => {
    if (g.game.hidden) return false;
    if (query && !g.game.name.toLowerCase().includes(query.toLowerCase())) return false;
    if (filter === "withSaves" && g.saveLocations.length === 0) return false;
    if (filter === "withBackups" && g.snapshots.length === 0) return false;
    if (filter === "withoutBackups" && g.snapshots.length > 0) return false;
    return true;
  });

  if (!scanned && games.length === 0) {
    return <div className="p-10 text-center text-muted">{t("scan.never")}</div>;
  }
  if (filtered.length === 0) {
    return <div className="p-10 text-center text-muted">{t("scan.empty")}</div>;
  }
  return (
    <div className="grid grid-cols-2 gap-4 p-5 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6">
      {filtered.map((g) => (
        <GameTile key={g.game.id} view={g} onClick={() => onOpen(g)} />
      ))}
    </div>
  );
}
