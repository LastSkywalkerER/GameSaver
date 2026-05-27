import { useEffect, useMemo, useRef, useState } from "react";
import type { GameView } from "../api";
import { GameTile } from "../components/GameTile";
import { useT } from "../i18n";
import { useControllerButton, useControllerNav } from "../controller";

export function DashboardPage({
  games,
  onOpen,
  query,
  filter,
  sortBy,
  scanned,
}: {
  games: GameView[];
  onOpen: (g: GameView) => void;
  query: string;
  filter: string;
  sortBy: string;
  scanned: boolean;
}) {
  const t = useT();
  const filtered = useMemo(() => {
    const fil = games.filter((g) => {
      if (g.game.hidden) return false;
      if (query && !g.game.name.toLowerCase().includes(query.toLowerCase())) return false;
      if (filter === "withSaves" && g.saveLocations.length === 0) return false;
      if (filter === "withBackups" && g.snapshots.length === 0) return false;
      if (filter === "withoutBackups" && g.snapshots.length > 0) return false;
      return true;
    });
    const arr = [...fil];
    switch (sortBy) {
      case "recent":
        arr.sort((a, b) => (b.game.lastPlayedAt ?? 0) - (a.game.lastPlayedAt ?? 0));
        break;
      case "playtime":
        arr.sort((a, b) => (b.game.totalPlaySeconds ?? 0) - (a.game.totalPlaySeconds ?? 0));
        break;
      case "installSize": {
        // Sum measured install-root sizes across all installations. Games where
        // the background walker hasn't reported yet sort to the bottom (sz=0).
        const sz = (g: GameView) => g.installations.reduce((s, i) => s + (i.installDirSizeBytes ?? 0), 0);
        arr.sort((a, b) => sz(b) - sz(a));
        break;
      }
      case "name":
      default:
        arr.sort((a, b) => a.game.name.toLowerCase().localeCompare(b.game.name.toLowerCase()));
    }
    return arr;
  }, [games, query, filter, sortBy]);

  // ── Controller navigation ──────────────────────────────────────────────
  // Roving-focus model: only one tile in the grid is keyboard-focusable
  // (tabIndex=0); the rest are -1. d-pad/LS moves the active index, A
  // dispatches a click on the focused tile. We measure the actual rendered
  // grid columns to compute up/down moves — Tailwind's responsive grid
  // changes columns at sm/md/lg/xl breakpoints, so we can't hardcode it.
  const gridRef = useRef<HTMLDivElement | null>(null);
  const tileRefs = useRef<(HTMLButtonElement | null)[]>([]);
  const [active, setActive] = useState(0);

  // Clamp active when the filtered list shrinks (e.g. user typed a query).
  useEffect(() => {
    if (active >= filtered.length) setActive(Math.max(0, filtered.length - 1));
  }, [filtered.length, active]);

  // Move browser focus to the active tile so the ring shows up + scroll into view.
  useEffect(() => {
    const el = tileRefs.current[active];
    if (!el) return;
    el.focus({ preventScroll: true });
    el.scrollIntoView({ block: "nearest", behavior: "smooth" });
  }, [active, filtered.length]);

  function columnsNow(): number {
    const grid = gridRef.current;
    if (!grid) return 1;
    // computed style "grid-template-columns" is "Npx Npx Npx …" — split.
    const cols = getComputedStyle(grid).gridTemplateColumns;
    return Math.max(1, cols.split(" ").filter(Boolean).length);
  }

  useControllerNav((dir) => {
    if (filtered.length === 0) return;
    setActive((idx) => {
      const cols = columnsNow();
      switch (dir) {
        case "left":  return Math.max(0, idx - 1);
        case "right": return Math.min(filtered.length - 1, idx + 1);
        case "up":    return Math.max(0, idx - cols);
        case "down":  return Math.min(filtered.length - 1, idx + cols);
      }
    });
  });

  useControllerButton((btn) => {
    if (btn !== "a") return;
    const g = filtered[active];
    if (g) onOpen(g);
  });

  if (!scanned && games.length === 0) {
    return <div className="p-10 text-center text-muted">{t("scan.never")}</div>;
  }
  if (filtered.length === 0) {
    return <div className="p-10 text-center text-muted">{t("scan.empty")}</div>;
  }
  return (
    <div
      ref={gridRef}
      className="grid grid-cols-2 gap-4 p-5 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6"
    >
      {filtered.map((g, i) => (
        <GameTile
          key={g.game.id}
          view={g}
          ref={(el) => { tileRefs.current[i] = el; }}
          tabIndex={i === active ? 0 : -1}
          onClick={() => { setActive(i); onOpen(g); }}
        />
      ))}
    </div>
  );
}
