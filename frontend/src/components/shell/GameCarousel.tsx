// Horizontal scrolling carousel of game covers — the bottom strip of
// shell-mode UI. Active tile is larger and full-colour; inactive tiles
// shrink + desaturate slightly. The whole row translates to keep the
// active tile centred near the left edge.

import { useEffect, useRef } from "react";
import clsx from "clsx";
import { coverUrl, type GameView } from "../../api";

const ACTIVE_W = 220;   // px
const INACTIVE_W = 160; // px
const GAP = 24;         // px
const LEFT_PAD = 80;    // px — where the active tile sits from the left edge

export function GameCarousel({
  games,
  activeIdx,
  onSelect,
}: {
  games: GameView[];
  activeIdx: number;
  onSelect: (i: number) => void;
}) {
  const rowRef = useRef<HTMLDivElement | null>(null);

  // Translate the row so the active tile lands at LEFT_PAD.
  // Sum of widths of inactive tiles before active + gaps before it.
  useEffect(() => {
    const row = rowRef.current;
    if (!row) return;
    const before = activeIdx * (INACTIVE_W + GAP);
    const x = LEFT_PAD - before;
    row.style.transform = `translateX(${x}px)`;
  }, [activeIdx]);

  if (games.length === 0) return null;

  return (
    <div className="absolute inset-x-0 bottom-0 h-[38vh] select-none overflow-hidden pb-10">
      <div
        ref={rowRef}
        className="flex items-end gap-6 transition-transform duration-300 ease-out"
        style={{ paddingLeft: 0, willChange: "transform" }}
      >
        {games.map((g, i) => {
          const active = i === activeIdx;
          const cover = coverUrl(g.game.coverPath);
          return (
            <button
              key={g.game.id}
              onClick={() => onSelect(i)}
              className={clsx(
                "relative shrink-0 overflow-hidden rounded-2xl border-2 transition-all duration-300",
                active
                  ? "border-accent shadow-[0_15px_60px_rgba(124,92,255,0.55)]"
                  : "border-white/5 opacity-70 hover:opacity-100"
              )}
              style={{
                width:  active ? ACTIVE_W   : INACTIVE_W,
                height: (active ? ACTIVE_W  : INACTIVE_W) * 1.5,
              }}
            >
              {cover ? (
                <img
                  src={cover}
                  alt={g.game.name}
                  loading="lazy"
                  className={clsx(
                    "h-full w-full object-cover transition-all duration-300",
                    !active && "grayscale-[40%]"
                  )}
                />
              ) : (
                <div className="flex h-full w-full items-center justify-center bg-gradient-to-br from-[#1f222c] to-[#0f1015] p-2 text-center text-sm font-semibold text-gray-200">
                  {g.game.name}
                </div>
              )}
              {/* Label appears only on the active tile so the row stays clean. */}
              {active && (
                <div className="pointer-events-none absolute inset-x-0 bottom-0 bg-gradient-to-t from-black/90 to-transparent p-3 text-sm font-semibold text-white">
                  {g.game.name}
                </div>
              )}
            </button>
          );
        })}
      </div>
    </div>
  );
}
