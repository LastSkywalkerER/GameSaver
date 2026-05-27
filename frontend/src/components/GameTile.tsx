import clsx from "clsx";
import type { GameView } from "../api";
import { coverUrl, formatBytes, formatDuration, formatRelative } from "../api";
import { SourceBadge } from "./SourceBadge";
import { useTilePrefs } from "../tilePrefs";

/** Aggregate install dir sizes per drive letter. */
function installsByDrive(view: GameView): { drive: string; bytes: number }[] {
  const map = new Map<string, number>();
  for (const i of view.installations) {
    const b = i.installDirSizeBytes ?? 0;
    if (b <= 0) continue;
    const head = (i.rootPath || "").split(/[\\/]/)[0];
    if (!head || head.length < 2 || head[1] !== ":") continue;
    const drive = head.toUpperCase();
    map.set(drive, (map.get(drive) ?? 0) + b);
  }
  return Array.from(map.entries())
    .map(([drive, bytes]) => ({ drive, bytes }))
    .sort((a, b) => b.bytes - a.bytes);
}

export function GameTile({
  view,
  onClick,
}: {
  view: GameView;
  onClick: () => void;
}) {
  const prefs = useTilePrefs();
  const g = view.game;
  const cover = coverUrl(g.coverPath);
  const sources = Array.from(new Set(view.installations.map((i) => i.source)));
  const hasInstall = view.installations.length > 0;
  const hasSave = view.saveLocations.length > 0;
  const hasBackup = view.snapshots.length > 0;
  const saveOnly = !hasInstall && hasSave;
  const installOnly = hasInstall && !hasSave;
  // Snapshots come from the API ordered by created_at DESC, so [0] is newest.
  const lastBackupAt = hasBackup ? view.snapshots[0].createdAt : 0;
  const drives = installsByDrive(view);

  return (
    <button
      onClick={onClick}
      className={clsx(
        "group relative aspect-[2/3] w-full overflow-hidden rounded-xl2 border border-border bg-card text-left shadow-soft transition",
        "hover:border-accent/60 hover:shadow-[0_6px_30px_rgba(124,92,255,0.25)] hover:-translate-y-0.5"
      )}
    >
      {cover ? (
        <img
          src={cover}
          alt={g.name}
          loading="lazy"
          className={clsx(
            "absolute inset-0 h-full w-full object-cover transition-transform duration-300 group-hover:scale-[1.04]",
            saveOnly && "grayscale opacity-80 group-hover:grayscale-0 group-hover:opacity-100"
          )}
          onError={(e) => { (e.target as HTMLImageElement).style.display = "none"; }}
        />
      ) : null}
      {!cover && (
        <div className={clsx(
          "absolute inset-0 flex items-center justify-center p-3 text-center",
          saveOnly
            ? "bg-gradient-to-br from-zinc-700 to-zinc-900"
            : "bg-gradient-to-br from-[#1f222c] to-[#0f1015]"
        )}>
          <span className="text-sm font-semibold leading-snug text-gray-200">{g.name}</span>
        </div>
      )}

      {/* Bottom gradient + name + sources/status pills */}
      <div className="pointer-events-none absolute inset-x-0 bottom-0 bg-gradient-to-t from-black/85 via-black/40 to-transparent p-3">
        <div className="text-sm font-semibold leading-tight text-white drop-shadow">{g.name}</div>
        <div className="mt-1 flex flex-wrap gap-1">
          {prefs.showSources && sources.map((s) => (
            <SourceBadge key={s} source={s} />
          ))}
          {prefs.showNewBadges && saveOnly && (
            <span className="chip border-zinc-500 bg-zinc-800/80 text-zinc-200">только сейв</span>
          )}
          {prefs.showNewBadges && installOnly && (
            <span className="chip border-amber-500/60 bg-amber-900/40 text-amber-200">NEW</span>
          )}
        </div>
      </div>

      {/* Top-left: "metadata" chips with dates / durations */}
      <div className="absolute left-2 top-2 flex max-w-[60%] flex-col items-start gap-1">
        {prefs.showLastPlayed && g.lastPlayedAt ? (
          <span
            className="chip border-emerald-700/50 bg-emerald-900/60 text-emerald-200 backdrop-blur-sm"
            title={"Последний запуск: " + new Date((g.lastPlayedAt ?? 0) * 1000).toLocaleString()}
          >
            ▶ {formatRelative(g.lastPlayedAt)}
          </span>
        ) : null}
        {prefs.showPlaytime && g.totalPlaySeconds && g.totalPlaySeconds > 0 ? (
          <span
            className="chip border-sky-700/50 bg-sky-900/60 text-sky-200 backdrop-blur-sm"
            title="Сумма по всем сессиям"
          >
            ⏱ {formatDuration(g.totalPlaySeconds)}
          </span>
        ) : null}
        {prefs.showLastBackup && lastBackupAt ? (
          <span
            className="chip border-violet-700/50 bg-violet-900/60 text-violet-200 backdrop-blur-sm"
            title={"Последний бэкап: " + new Date(lastBackupAt * 1000).toLocaleString()}
          >
            ⛁ {formatRelative(lastBackupAt)}
          </span>
        ) : null}
        {prefs.showInstallSize && drives.map((d) => (
          <span
            key={d.drive}
            className="chip border-amber-700/50 bg-amber-900/60 text-amber-200 backdrop-blur-sm"
            title={`Установлено на ${d.drive}\\ — занимает ${formatBytes(d.bytes)}`}
          >
            💾 {d.drive} {formatBytes(d.bytes)}
          </span>
        ))}
      </div>

      {/* Top-right: count chips (existing) */}
      <div className="absolute right-2 top-2 flex flex-col gap-1">
        {prefs.showSaveCount ? (
          hasSave ? (
            <span className="chip border-emerald-700/50 bg-emerald-900/50 text-emerald-300">
              ✓ {view.saveLocations.length}
            </span>
          ) : (
            <span className="chip border-zinc-700 bg-zinc-900/50 text-zinc-400">—</span>
          )
        ) : null}
        {prefs.showBackupCount && hasBackup ? (
          <span className="chip border-violet-700/50 bg-violet-900/50 text-violet-200">
            ⛁ {view.snapshots.length}
          </span>
        ) : null}
      </div>
    </button>
  );
}
