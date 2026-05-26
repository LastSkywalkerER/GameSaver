import type { GameView } from "../api";
import { formatBytes, formatDate } from "../api";
import { SourceBadge } from "../components/SourceBadge";

export function LibraryPage({ games, onOpen }: { games: GameView[]; onOpen: (g: GameView) => void }) {
  return (
    <div className="p-5">
      <table className="w-full text-sm">
        <thead className="text-left text-muted">
          <tr>
            <th className="px-2 py-2">Name</th>
            <th className="px-2 py-2">Sources</th>
            <th className="px-2 py-2">Saves</th>
            <th className="px-2 py-2">Last backup</th>
            <th className="px-2 py-2 text-right">Size</th>
          </tr>
        </thead>
        <tbody>
          {games.map((g) => {
            const totalSave = g.saveLocations.reduce((a, s) => a + s.sizeBytes, 0);
            const lastSnap = g.snapshots[0]; // ordered DESC
            return (
              <tr
                key={g.game.id}
                onClick={() => onOpen(g)}
                className="cursor-pointer border-t border-border hover:bg-panel"
              >
                <td className="px-2 py-2 font-medium text-gray-100">{g.game.name}</td>
                <td className="px-2 py-2">
                  <div className="flex flex-wrap gap-1">
                    {Array.from(new Set(g.installations.map((i) => i.source))).map((s) => (
                      <SourceBadge key={s} source={s} />
                    ))}
                  </div>
                </td>
                <td className="px-2 py-2 text-gray-300">{g.saveLocations.length}</td>
                <td className="px-2 py-2 text-gray-300">{lastSnap ? formatDate(lastSnap.createdAt) : "—"}</td>
                <td className="px-2 py-2 text-right text-gray-300">{formatBytes(totalSave)}</td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}
