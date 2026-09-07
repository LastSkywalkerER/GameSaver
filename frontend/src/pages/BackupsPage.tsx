import { useEffect, useMemo, useState } from "react";
import { api, type GameView, type Platform, type SwitchBackup } from "../api";
import { formatBytes, formatDate } from "../api";

// One row in the unified list. Windows snapshots come from the DB (they have
// game/location rows); console snapshots are read off disk, since they
// deliberately have no DB rows — see internal/switchsaves.ListBackups.
type Row = {
  key: string;
  platform: Platform;
  gameName: string;
  detail: string; // console profile, or the backup trigger on Windows
  createdAt: number;
  fileCount: number;
  sizeBytes: number;
  archivePath: string;
};

function PlatformBadge({ platform }: { platform: Platform }) {
  const isSwitch = platform === "switch";
  return (
    <span
      className={
        "mr-2 rounded px-1.5 py-0.5 text-[10px] " +
        (isSwitch ? "bg-red-900/40 text-red-200" : "bg-sky-900/40 text-sky-200")
      }
      title={isSwitch ? "Снят с консоли Nintendo Switch" : "Снят с этого ПК"}
    >
      {isSwitch ? "Switch" : "Windows"}
    </span>
  );
}

export function BackupsPage({ games }: { games: GameView[] }) {
  const [switchBackups, setSwitchBackups] = useState<SwitchBackup[]>([]);
  const [filter, setFilter] = useState<"all" | Platform>("all");

  useEffect(() => {
    (api as any).ListSwitchBackups?.()
      .then((b: any) => setSwitchBackups((b ?? []) as SwitchBackup[]))
      .catch(() => {});
  }, []);

  const rows = useMemo(() => {
    const out: Row[] = [];
    for (const g of games) {
      for (const sn of g.snapshots ?? []) {
        out.push({
          key: sn.id,
          platform: "windows",
          gameName: g.game.name,
          detail: sn.trigger,
          createdAt: sn.createdAt,
          fileCount: sn.fileCount,
          sizeBytes: sn.compressedBytes,
          archivePath: sn.archivePath,
        });
      }
    }
    for (const b of switchBackups) {
      out.push({
        key: b.archivePath,
        platform: "switch",
        gameName: b.title,
        detail: `профиль ${b.profile}`,
        createdAt: b.createdAt,
        fileCount: b.fileCount,
        sizeBytes: b.sizeOnDisk,
        archivePath: b.archivePath,
      });
    }
    out.sort((a, b) => b.createdAt - a.createdAt);
    return filter === "all" ? out : out.filter((r) => r.platform === filter);
  }, [games, switchBackups, filter]);

  const counts = useMemo(() => {
    let win = 0;
    for (const g of games) win += (g.snapshots ?? []).length;
    return { win, sw: switchBackups.length };
  }, [games, switchBackups]);

  return (
    <div className="p-5">
      <div className="mb-3 flex items-center gap-2 text-xs">
        {(["all", "windows", "switch"] as const).map((f) => (
          <button
            key={f}
            className={"btn !px-2 !py-0.5 !text-xs " + (filter === f ? "btn-primary" : "")}
            onClick={() => setFilter(f)}
          >
            {f === "all" ? `Все (${counts.win + counts.sw})`
              : f === "windows" ? `Windows (${counts.win})`
              : `Switch (${counts.sw})`}
          </button>
        ))}
      </div>

      {rows.length === 0 ? (
        <div className="p-10 text-center text-muted">Снэпшотов пока нет.</div>
      ) : (
        <table className="w-full text-sm">
          <thead className="text-left text-muted">
            <tr>
              <th className="px-2 py-2">Игра</th>
              <th className="px-2 py-2">Когда</th>
              <th className="px-2 py-2">Источник</th>
              <th className="px-2 py-2 text-right">Файлов</th>
              <th className="px-2 py-2 text-right">Размер</th>
              <th className="px-2 py-2">Архив</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((r) => (
              <tr key={r.key} className="border-t border-border">
                <td className="px-2 py-2 font-medium text-gray-100">
                  <PlatformBadge platform={r.platform} />
                  {r.gameName}
                </td>
                <td className="px-2 py-2 text-gray-300">{formatDate(r.createdAt)}</td>
                <td className="px-2 py-2 text-gray-300">{r.detail}</td>
                <td className="px-2 py-2 text-right text-gray-300">{r.fileCount}</td>
                <td className="px-2 py-2 text-right text-gray-300">{formatBytes(r.sizeBytes)}</td>
                <td className="px-2 py-2 truncate text-xs text-muted" title={r.archivePath}>
                  {r.archivePath}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  );
}
