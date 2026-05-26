import { useMemo } from "react";
import type { GameView } from "../api";
import { formatBytes, formatDate } from "../api";

export function BackupsPage({ games }: { games: GameView[] }) {
  const flat = useMemo(() => {
    const rows: { gameName: string; sn: GameView["snapshots"][number] }[] = [];
    for (const g of games) {
      for (const sn of g.snapshots) rows.push({ gameName: g.game.name, sn });
    }
    rows.sort((a, b) => b.sn.createdAt - a.sn.createdAt);
    return rows;
  }, [games]);
  if (flat.length === 0) {
    return <div className="p-10 text-center text-muted">Снэпшотов пока нет.</div>;
  }
  return (
    <div className="p-5">
      <table className="w-full text-sm">
        <thead className="text-left text-muted">
          <tr>
            <th className="px-2 py-2">Game</th>
            <th className="px-2 py-2">When</th>
            <th className="px-2 py-2">Trigger</th>
            <th className="px-2 py-2 text-right">Files</th>
            <th className="px-2 py-2 text-right">Size</th>
            <th className="px-2 py-2">Archive</th>
          </tr>
        </thead>
        <tbody>
          {flat.map(({ gameName, sn }) => (
            <tr key={sn.id} className="border-t border-border">
              <td className="px-2 py-2 font-medium text-gray-100">{gameName}</td>
              <td className="px-2 py-2 text-gray-300">{formatDate(sn.createdAt)}</td>
              <td className="px-2 py-2 text-gray-300">{sn.trigger}</td>
              <td className="px-2 py-2 text-right text-gray-300">{sn.fileCount}</td>
              <td className="px-2 py-2 text-right text-gray-300">{formatBytes(sn.compressedBytes)}</td>
              <td className="px-2 py-2 truncate text-xs text-muted" title={sn.archivePath}>{sn.archivePath}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
