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

import { useEffect, useMemo, useState } from "react";
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

  // ── Controller navigation ────────────────────────────────────────────
  useControllerNav((dir) => {
    if (overlay !== "none") return; // overlay has its own input (or none yet)
    if (dir === "left") {
      setActiveIdx((i) => {
        const next = Math.max(0, i - 1);
        if (next !== i) playMove();
        return next;
      });
    } else if (dir === "right") {
      setActiveIdx((i) => {
        const next = Math.min(sorted.length - 1, i + 1);
        if (next !== i) playMove();
        return next;
      });
    }
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
