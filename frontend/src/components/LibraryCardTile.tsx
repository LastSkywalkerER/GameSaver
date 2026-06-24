import clsx from "clsx";
import type { LibraryCard } from "../api";
import { coverUrl } from "../api";
import { SourceBadge } from "./SourceBadge";

// Steam owned-but-not-installed games have no local cover; fall back to the
// public Steam CDN portrait using the appid (no backend enrichment needed).
function steamCdnCover(appId?: number): string | undefined {
  if (!appId) return undefined;
  return `https://cdn.cloudflare.steamstatic.com/steam/apps/${appId}/library_600x900.jpg`;
}

// LibraryCardTile — one merged game across stores/accounts: cover, name, and a
// row of (store · account · installed?) chips. Greyed when owned nowhere
// installed. Two facts of the same store/different account render separately.
export function LibraryCardTile({
  card,
  onClick,
  rating = 0,
  onRate,
}: {
  card: LibraryCard;
  onClick?: () => void;
  rating?: number;
  onRate?: (stars: number) => void;
}) {
  const g = card.game;
  const cover = coverUrl(g.coverPath) ?? steamCdnCover((g as any).steamAppId) ?? card.imageUrl;
  const installed = card.anyInstalled;

  return (
    <button
      onClick={onClick}
      className={clsx(
        "group relative aspect-[2/3] w-full overflow-hidden rounded-xl2 border border-border bg-card text-left shadow-soft transition",
        "hover:-translate-y-0.5 hover:border-accent/60 hover:shadow-[0_6px_30px_rgba(124,92,255,0.25)]",
        "focus:outline-none focus-visible:ring-4 focus-visible:ring-accent",
      )}
    >
      {cover ? (
        <img
          src={cover}
          alt={g.name}
          loading="lazy"
          className="absolute inset-0 h-full w-full object-cover transition-transform duration-300 group-hover:scale-[1.04]"
          onError={(e) => { (e.target as HTMLImageElement).style.display = "none"; }}
        />
      ) : (
        <div className="absolute inset-0 flex items-center justify-center bg-gradient-to-br from-[#1f222c] to-[#0f1015] p-3 text-center">
          <span className="text-sm font-semibold leading-snug text-gray-200">{g.name}</span>
        </div>
      )}

      <div className="absolute right-2 top-2">
        {installed ? (
          <span className="chip border-emerald-700/50 bg-emerald-900/70 text-emerald-200 backdrop-blur-sm">установлена</span>
        ) : (
          <span className="chip border-amber-600/50 bg-amber-900/60 text-amber-200 backdrop-blur-sm">куплена</span>
        )}
      </div>

      <div className="pointer-events-none absolute inset-x-0 bottom-0 bg-gradient-to-t from-black/85 via-black/40 to-transparent p-3">
        <div className="text-sm font-semibold leading-tight text-white drop-shadow">{g.name}</div>
        <div className="mt-1 flex flex-wrap gap-1">
          {card.ownership.map((f, i) => (
            <span key={f.accountId + ":" + i} className="inline-flex items-center gap-1">
              <SourceBadge source={f.store} />
              <span
                className={clsx(
                  "chip border-white/10 text-[10px]",
                  f.installed ? "bg-emerald-900/50 text-emerald-200" : "bg-black/40 text-gray-300",
                )}
                title={f.installed ? "установлена на этот аккаунт" : "куплена, не установлена"}
              >
                {f.accountName || "?"}{f.installed ? " ✓" : ""}
              </span>
            </span>
          ))}
          {card.ownership.length === 0 &&
            Array.from(new Set(card.installations.map((i) => i.source))).map((s) => (
              <SourceBadge key={s} source={s} />
            ))}
        </div>
        {onRate && (
          <div className="pointer-events-auto mt-1.5 flex gap-0.5">
            {[1, 2, 3, 4, 5].map((n) => (
              <span
                key={n}
                role="button"
                title={`Оценка ${n}/5`}
                onClick={(e) => { e.stopPropagation(); e.preventDefault(); onRate(n === rating ? 0 : n); }}
                className={clsx(
                  "cursor-pointer text-lg leading-none drop-shadow transition-transform hover:scale-110",
                  n <= rating ? "text-amber-400" : "text-gray-400/70 hover:text-amber-300",
                )}
              >
                ★
              </span>
            ))}
          </div>
        )}
      </div>
    </button>
  );
}
