// ShellApp — the top-level immersive UI shown when GameSaver is running
// as the Windows shell (under gamesaver-watchdog.exe). Completely separate
// from the regular desktop UI: animated background, hero panel, carousel
// of tiles, corner icons. Designed to be controller-driven first.
//
// Controller bindings (in addition to the global "B closes drawer, Start
// cycles overlays" already in App.tsx):
//   d-pad / LS left, right    move carousel  +  playMove()
//   A                          launch active  +  playSelect()
//   Y                          open Details   +  playSelect()
//   B                          close overlay  +  playBack()

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { api, type GameView } from "../../api";
import { useControllerButton, useControllerNav } from "../../controller";
import { playBack, playMove, playSelect } from "../../sound";
import { GameDrawer } from "../GameDrawer";
import { BackupsPage } from "../../pages/BackupsPage";
import { SettingsPage } from "../../pages/SettingsPage";
import { Modal } from "../Modal";
import { CornerIcons } from "./CornerIcons";
import { GameCarousel } from "./GameCarousel";
import { HeroPanel } from "./HeroPanel";
import { ShellBackground } from "./ShellBackground";

type Overlay = "none" | "details" | "settings" | "backups";

export function ShellApp({
  games,
  refresh,
}: {
  games: GameView[];
  refresh: () => void;
}) {
  // Sort: recent-played first so the most-likely-to-play game lands under
  // the cursor on logon. Falls back to alphabetical for never-played games.
  const sorted = useMemo(() => {
    const arr = games.filter((g) => !g.game.hidden);
    return arr.sort((a, b) => {
      const la = a.game.lastPlayedAt ?? 0;
      const lb = b.game.lastPlayedAt ?? 0;
      if (la !== lb) return lb - la;
      return a.game.name.localeCompare(b.game.name);
    });
  }, [games]);

  const [activeIdx, setActiveIdx] = useState(0);
  const [overlay, setOverlay] = useState<Overlay>("none");

  // Keep activeIdx in range when the list shrinks (e.g. a hidden flag flips).
  useEffect(() => {
    if (activeIdx >= sorted.length) setActiveIdx(Math.max(0, sorted.length - 1));
  }, [sorted.length, activeIdx]);

  const active = sorted[activeIdx] ?? null;

  // Shared cursor mover — used by controller nav, keyboard arrows, and
  // mouse wheel. Plays the move sound only when the cursor actually
  // shifts (so holding ← at the leftmost tile doesn't tick endlessly).
  const moveCursor = useCallback((delta: number) => {
    setActiveIdx((i) => {
      const next = Math.max(0, Math.min(sorted.length - 1, i + delta));
      if (next !== i) playMove();
      return next;
    });
  }, [sorted.length]);

  // ── Controller navigation ────────────────────────────────────────────
  useControllerNav((dir) => {
    if (overlay !== "none") return; // overlay has its own input (or none yet)
    if (dir === "left")  moveCursor(-1);
    if (dir === "right") moveCursor(+1);
  });

  useControllerButton((btn) => {
    if (overlay !== "none") {
      if (btn === "b") {
        playBack();
        setOverlay("none");
      }
      return;
    }
    if (btn === "a" && active) {
      doLaunch(active);
    } else if (btn === "y" && active) {
      playSelect();
      setOverlay("details");
    } else if (btn === "start") {
      playSelect();
      setOverlay("settings");
    } else if (btn === "back") {
      playSelect();
      setOverlay("backups");
    }
  });

  // ── Keyboard navigation ─────────────────────────────────────────────
  // Arrow keys mirror d-pad, Enter mirrors A, Escape mirrors B. Lets
  // mouse-and-keyboard users drive the shell UI without a controller.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      // Don't hijack typing inside the Settings overlay's inputs etc.
      const target = e.target as HTMLElement | null;
      if (target && (target.tagName === "INPUT" || target.tagName === "TEXTAREA" || target.isContentEditable)) {
        return;
      }
      if (overlay !== "none") {
        if (e.key === "Escape") { e.preventDefault(); playBack(); setOverlay("none"); }
        return;
      }
      if (e.key === "ArrowLeft")  { e.preventDefault(); moveCursor(-1); }
      else if (e.key === "ArrowRight") { e.preventDefault(); moveCursor(+1); }
      else if (e.key === "Enter" && active) { e.preventDefault(); doLaunch(active); }
      else if (e.key === "Escape" || e.key.toLowerCase() === "i") {
        // 'i' for "info" — mouse-keyboard counterpart of controller's Y.
        if (e.key.toLowerCase() === "i" && active) {
          e.preventDefault();
          playSelect();
          setOverlay("details");
        }
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [overlay, active, moveCursor]);

  // ── Mouse-wheel navigation ──────────────────────────────────────────
  // One wheel notch (or one trackpad scroll-step) advances the carousel
  // by one tile. Wheel events fire fast on trackpads — throttle to ~150 ms
  // so a single swipe doesn't shoot past 5 games.
  const wheelLockRef = useRef(0);
  useEffect(() => {
    const onWheel = (e: WheelEvent) => {
      if (overlay !== "none") return;
      const now = Date.now();
      if (now - wheelLockRef.current < 150) return;
      // deltaY > 0 = scroll down/forward → next tile.
      // We also accept horizontal wheel (deltaX) for trackpads doing
      // left/right scroll gestures.
      const d = Math.abs(e.deltaX) > Math.abs(e.deltaY) ? e.deltaX : e.deltaY;
      if (d === 0) return;
      wheelLockRef.current = now;
      moveCursor(d > 0 ? +1 : -1);
    };
    window.addEventListener("wheel", onWheel, { passive: true });
    return () => window.removeEventListener("wheel", onWheel);
  }, [overlay, moveCursor]);

  async function doLaunch(g: GameView) {
    playSelect();
    const inst = g.installations[0];
    if (!inst) {
      api.Toast("error", "У игры нет установок");
      return;
    }
    try {
      await api.LaunchGame(g.game.id, inst.id);
    } catch (e) {
      api.Toast("error", "Не удалось запустить: " + String(e));
    }
  }

  return (
    <div className="fixed inset-0 overflow-hidden text-gray-100">
      <ShellBackground />
      <CornerIcons
        onSettings={() => { playSelect(); setOverlay("settings"); }}
        onBackups={() => { playSelect(); setOverlay("backups"); }}
        onExit={() => { playBack(); api.QuitApp(); }}
      />

      <HeroPanel
        game={active}
        onLaunch={() => active && doLaunch(active)}
        onDetails={() => { playSelect(); setOverlay("details"); }}
      />
      <GameCarousel
        games={sorted}
        activeIdx={activeIdx}
        onSelect={(i) => { if (i !== activeIdx) playMove(); setActiveIdx(i); }}
      />

      {overlay === "details" && active && (
        <GameDrawer
          view={active}
          onClose={() => { playBack(); setOverlay("none"); }}
          onChanged={refresh}
        />
      )}
      <Modal
        open={overlay === "settings"}
        title="Настройки"
        onClose={() => { playBack(); setOverlay("none"); }}
      >
        <div className="max-h-[70vh] overflow-y-auto">
          <SettingsPage />
        </div>
      </Modal>
      <Modal
        open={overlay === "backups"}
        title="Бэкапы"
        onClose={() => { playBack(); setOverlay("none"); }}
      >
        <div className="max-h-[70vh] overflow-y-auto">
          <BackupsPage games={games} />
        </div>
      </Modal>
    </div>
  );
}
