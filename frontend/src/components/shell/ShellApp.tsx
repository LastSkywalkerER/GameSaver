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
import { api, EventsOn, type GameView } from "../../api";
import { useControllerButton, useControllerConnected, useControllerNav } from "../../controller";
import { playBack, playMove, playSelect } from "../../sound";
import { GameDrawer } from "../GameDrawer";
import { BackupsPage } from "../../pages/BackupsPage";
import { SettingsPage } from "../../pages/SettingsPage";
import { Modal } from "../Modal";
import { CornerIcons } from "./CornerIcons";
import { GameCarousel } from "./GameCarousel";
import { HeroPanel } from "./HeroPanel";
import { ShellBackground } from "./ShellBackground";
import { MonitorPicker, type Monitor } from "./MonitorPicker";
import { PowerMenu } from "./PowerMenu";

type Overlay = "none" | "details" | "settings" | "backups" | "power";

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
  const padOn = useControllerConnected();

  // Monitor picker — on first ShellApp mount we ask the OS how many
  // displays are attached. If 2+, we show the picker overlay. The user's
  // previous choice (per gs:soleMonitorId in localStorage) is auto-applied
  // silently — they only see the picker once unless they explicitly clear
  // it. If only 1 monitor is attached we skip entirely.
  const [monitorsToPick, setMonitorsToPick] = useState<Monitor[] | null>(null);

  // Initial check at mount + re-check on every hot-plug ("display:changed").
  // Logic: if there's a remembered choice AND it's still in the active list,
  // silently re-apply (typical case: power-on, single-monitor steady state).
  // Otherwise, surface the picker so the user can choose.
  useEffect(() => {
    let cancelled = false;

    async function evaluate(mons: Monitor[]) {
      if (cancelled) return;
      if (!Array.isArray(mons) || mons.length < 2) {
        setMonitorsToPick(null);
        return;
      }
      const remembered = (() => { try { return localStorage.getItem("gs:soleMonitorId") || ""; } catch { return ""; } })();
      if (remembered && mons.some((m) => m.id === remembered)) {
        // Re-apply silently — user already picked this, no need to ask again.
        try { await api.MakeSoleMonitor(remembered); } catch (e) { console.warn("re-apply monitor failed", e); }
        setMonitorsToPick(null);
        return;
      }
      // Either no remembered choice, or it's gone (e.g. the monitor was
      // unplugged) — reopen the picker.
      setMonitorsToPick(mons);
    }

    (async () => {
      try { const mons: any = await api.ListMonitors(); await evaluate(mons as Monitor[]); }
      catch (e) { console.warn("ListMonitors failed", e); }
    })();

    const off = EventsOn("display:changed", (payload: any) => {
      // payload is the new monitor list — saves us another round-trip.
      evaluate(payload as Monitor[]);
    });
    return () => {
      cancelled = true;
      try { (off as any)?.(); } catch {}
    };
  }, []);

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
  // While the monitor picker is up, every input goes to it — otherwise a
  // d-pad left in the picker would also shift the carousel cursor behind
  // it, and A would launch a game instead of confirming the picker.
  const inputBlocked = overlay !== "none" || monitorsToPick !== null;
  useControllerNav((dir) => {
    if (inputBlocked) return;
    if (dir === "left")  moveCursor(-1);
    if (dir === "right") moveCursor(+1);
  });

  useControllerButton((btn) => {
    if (monitorsToPick !== null) return; // picker owns the controller
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
    } else if (btn === "x") {
      // X is the only otherwise-unused face button — bind it to the
      // power menu so Lock/Sleep/Exit are reachable from controller.
      playSelect();
      setOverlay("power");
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
      if (monitorsToPick !== null) return; // picker owns the keyboard
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
  }, [overlay, active, moveCursor, monitorsToPick]);

  // ── Mouse-wheel navigation ──────────────────────────────────────────
  // One wheel notch (or one trackpad scroll-step) advances the carousel
  // by one tile. Wheel events fire fast on trackpads — throttle to ~150 ms
  // so a single swipe doesn't shoot past 5 games.
  const wheelLockRef = useRef(0);
  useEffect(() => {
    const onWheel = (e: WheelEvent) => {
      if (overlay !== "none" || monitorsToPick !== null) return;
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
  }, [overlay, moveCursor, monitorsToPick]);

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

      {/* Top-left: controller status + button hints. The hint row only
          renders when a controller is connected — without one, hints are
          irrelevant (keyboard hints would be wrong) and we keep the
          corner clean. The 🎮 chip sits inside even when no controller
          is detected so the user gets a clear "not connected" signal
          rather than wondering why their pad doesn't work. */}
      <div className="absolute left-6 top-6 z-20 flex items-center gap-3">
        <span
          className={
            "flex items-center gap-2 rounded-full border px-3 py-1.5 text-sm backdrop-blur-md transition " +
            (padOn
              ? "border-emerald-500/40 bg-emerald-900/40 text-emerald-200"
              : "border-white/10 bg-white/5 text-gray-400")
          }
          title={padOn ? "Xbox-совместимый контроллер подключён" : "Контроллер не подключён — пользуйся клавой/мышью"}
        >
          🎮 {padOn ? "controller" : "no pad"}
        </span>
        {padOn && (
          <span className="hidden gap-4 rounded-full border border-white/10 bg-white/5 px-4 py-1.5 text-xs text-gray-300 backdrop-blur-md sm:flex">
            <span><kbd className="rounded bg-white/10 px-1.5 py-0.5">A</kbd> запустить</span>
            <span><kbd className="rounded bg-white/10 px-1.5 py-0.5">Y</kbd> подробнее</span>
            <span><kbd className="rounded bg-white/10 px-1.5 py-0.5">X</kbd> питание</span>
            <span><kbd className="rounded bg-white/10 px-1.5 py-0.5">Start</kbd> настройки</span>
            <span><kbd className="rounded bg-white/10 px-1.5 py-0.5">Back</kbd> бэкапы</span>
          </span>
        )}
      </div>

      <CornerIcons
        onPower={() => { playSelect(); setOverlay("power"); }}
        onSettings={() => { playSelect(); setOverlay("settings"); }}
        onBackups={() => { playSelect(); setOverlay("backups"); }}
        onExit={async () => {
          playBack();
          // Bring the other monitors back before we hand control back to
          // Explorer — otherwise the user logs in to a single-screen
          // setup and has to fix it in Windows display settings.
          try { await api.RestoreMonitorConfig(); } catch (e) { console.warn("restore monitors", e); }
          try { localStorage.removeItem("gs:soleMonitorId"); } catch {}
          api.QuitApp();
        }}
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

      {monitorsToPick && (
        <MonitorPicker
          monitors={monitorsToPick}
          onDone={() => setMonitorsToPick(null)}
        />
      )}

      {overlay === "power" && (
        <PowerMenu
          onClose={() => setOverlay("none")}
          onExit={async () => {
            try { await api.RestoreMonitorConfig(); } catch (e) { console.warn("restore monitors", e); }
            try { localStorage.removeItem("gs:soleMonitorId"); } catch {}
            api.QuitApp();
          }}
        />
      )}
    </div>
  );
}
