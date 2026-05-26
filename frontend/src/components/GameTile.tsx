import clsx from "clsx";
import type { GameView } from "../api";
import { coverUrl } from "../api";
import { SourceBadge } from "./SourceBadge";

export function GameTile({
  view,
  onClick,
}: {
  view: GameView;
  onClick: () => void;
}) {
  const g = view.game;
  const cover = coverUrl(g.coverPath);
  const sources = Array.from(new Set(view.installations.map((i) => i.source)));
  const hasInstall = view.installations.length > 0;
  const hasSave = view.saveLocations.length > 0;
  const hasBackup = view.snapshots.length > 0;
  // Visual hints:
  //  - save-only (no install): grayscale + "только сейв" tag
  //  - install-only (no save): "NEW" tag suggesting "ready to play, no save yet"
  const saveOnly = !hasInstall && hasSave;
  const installOnly = hasInstall && !hasSave;
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
      <div className="pointer-events-none absolute inset-x-0 bottom-0 bg-gradient-to-t from-black/85 via-black/40 to-transparent p-3">
        <div className="text-sm font-semibold leading-tight text-white drop-shadow">{g.name}</div>
        <div className="mt-1 flex flex-wrap gap-1">
          {sources.map((s) => (
            <SourceBadge key={s} source={s} />
          ))}
          {saveOnly && (
            <span className="chip border-zinc-500 bg-zinc-800/80 text-zinc-200">только сейв</span>
          )}
          {installOnly && (
            <span className="chip border-amber-500/60 bg-amber-900/40 text-amber-200">NEW</span>
          )}
        </div>
      </div>
      <div className="absolute right-2 top-2 flex flex-col gap-1">
        {hasSave ? (
          <span className="chip border-emerald-700/50 bg-emerald-900/50 text-emerald-300">
            ✓ {view.saveLocations.length}
          </span>
        ) : (
          <span className="chip border-zinc-700 bg-zinc-900/50 text-zinc-400">
            —
          </span>
        )}
        {hasBackup && (
          <span className="chip border-violet-700/50 bg-violet-900/50 text-violet-200">
            ⛁ {view.snapshots.length}
          </span>
        )}
      </div>
    </button>
  );
}
