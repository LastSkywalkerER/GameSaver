// ShellApp — the top-level immersive UI shown when GameSaver is running
// as the Windows shell (under gamesaver-watchdog.exe). Completely separate
// from the regular desktop UI: animated background, hero panel, carousel
// of tiles, corner icons. Designed to be controller-driven first.
//
// v0.10.0 controller layout (rule #1: every UI surface reachable from
// the gamepad):
//   d-pad ← →        move carousel  +  playMove()
//   d-pad ↑          jump focus from carousel up to the corner icons row
//   d-pad ↓          jump focus from corner icons back down to carousel
//   A                if on carousel: launch active game.
//                    if on corner icons: fire the focused icon.
//   Y                open Settings (full-screen shell page, not modal)
//   X                open Game Details drawer for the active game
//   Start            open Power menu (lock / sleep / reboot / exit)
//   Back / Select    open Devices chooser (monitor / audio / Bluetooth)
//   B                close whatever overlay is on top; otherwise no-op
//
// The Backups modal still exists but it's reached via the ⛁ corner
// icon, not a controller face button — picking a monitor / output
// is far more frequent in daily use than browsing backups.

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { api, EventsOn, type GameView, type UpdateInfo } from "../../api";
import { useControllerButton, useControllerConnected, useControllerNav } from "../../controller";
import {
  getSoundPack,
  playBack,
  playMove,
  playSelect,
  startAmbient,
  stopAmbient,
  subscribeSoundPack,
} from "../../sound";
import { GameDrawer } from "../GameDrawer";
import { ShellBackupsView } from "./ShellBackupsView";
import { CornerIcons, CORNER_ICON_ORDER } from "./CornerIcons";
import { GameCarousel } from "./GameCarousel";
import { HeroPanel } from "./HeroPanel";
import { ShellBackground } from "./ShellBackground";
import { ShellSettingsPage } from "./ShellSettingsPage";
import { MonitorPicker, type PickPrep } from "./MonitorPicker";
import { AudioPicker } from "./AudioPicker";
import { BluetoothPicker } from "./BluetoothPicker";
import { VolumePicker } from "./VolumePicker";
import { DevicesMenu } from "./DevicesMenu";
import { PowerMenu } from "./PowerMenu";
import { ShellUpdateModal } from "./ShellUpdateModal";

type Overlay = "none" | "details" | "settings" | "backups" | "power" | "devices";

export function ShellApp({
  games,
  refresh,
  update,
  onDismissUpdate,
}: {
  games: GameView[];
  refresh: () => void;
  update: UpdateInfo | null;
  onDismissUpdate: () => void;
}) {
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
  // Which row owns d-pad-left/right: -1 = carousel, 0..N-1 = corner icons.
  // d-pad ↑ on the carousel jumps to the icon row; d-pad ↓ on the icons
  // jumps back to the carousel.
  const [cornerFocus, setCornerFocus] = useState<number>(-1);
  const padOn = useControllerConnected();

  const [pickPrep, setPickPrep] = useState<PickPrep | null>(null);
  const pickerOpen = pickPrep !== null;

  // Audio picker is now opened either as the second leg of the on-boot
  // monitor→audio chain, or via the Devices chooser modal. The corner
  // icon for audio is gone in v0.10.0 — Devices is one button now.
  const [audioOpen, setAudioOpen] = useState(false);
  const audioAutoChainRef = useRef<boolean>(true);
  const [btOpen, setBtOpen] = useState(false);
  const [volOpen, setVolOpen] = useState(false);

  const openPicker = useCallback(async () => {
    try {
      const prep: any = await api.PrepareMonitorPick();
      if (prep && Array.isArray(prep.monitors) && prep.monitors.length >= 2) {
        setPickPrep(prep as PickPrep);
      } else {
        try { await api.CancelMonitorPick(); } catch {}
        setPickPrep(null);
        if (audioAutoChainRef.current) {
          audioAutoChainRef.current = false;
          setAudioOpen(true);
        }
      }
    } catch (e) {
      console.warn("PrepareMonitorPick failed", e);
    }
  }, []);

  // After the user commits a monitor, MakeSole disabling the others
  // changes the topology — which our display.Watch picks up and would
  // otherwise treat as "re-show the picker", looping forever. 30s
  // settling window for re-assertion, 20s suppression for sleep/lock.
  const committedRef = useRef<string>("");
  const lastCommitRef = useRef<number>(0);
  const changeTimerRef = useRef<number | null>(null);
  const SETTLE_MS = 30000;
  const suppressUntilRef = useRef<number>(0);
  const armSuppress = useCallback(() => { suppressUntilRef.current = Date.now() + 20000; }, []);

  useEffect(() => {
    openPicker();
    const handle = () => {
      if (changeTimerRef.current) window.clearTimeout(changeTimerRef.current);
      changeTimerRef.current = window.setTimeout(async () => {
        if (Date.now() < suppressUntilRef.current) return;
        const sincePick = Date.now() - lastCommitRef.current;
        if (committedRef.current && sincePick < SETTLE_MS) {
          try { await api.MakeSoleMonitor(committedRef.current); } catch (e) { console.warn("re-assert monitor", e); }
          return;
        }
        openPicker();
      }, 3000);
    };
    const off = EventsOn("display:changed", handle);
    return () => {
      try { (off as any)?.(); } catch {}
      if (changeTimerRef.current) window.clearTimeout(changeTimerRef.current);
    };
  }, [openPicker]);

  useEffect(() => {
    if (activeIdx >= sorted.length) setActiveIdx(Math.max(0, sorted.length - 1));
  }, [sorted.length, activeIdx]);

  const active = sorted[activeIdx] ?? null;

  const moveCursor = useCallback((delta: number) => {
    setActiveIdx((i) => {
      const next = Math.max(0, Math.min(sorted.length - 1, i + delta));
      if (next !== i) playMove();
      return next;
    });
  }, [sorted.length]);

  // Walk the corner-icon row. Same 100 ms debounce as everywhere else.
  const lastIconMove = useRef(0);
  const moveCornerFocus = useCallback((delta: number) => {
    const now = Date.now();
    if (now - lastIconMove.current < 100) return;
    lastIconMove.current = now;
    setCornerFocus((i) => {
      const max = CORNER_ICON_ORDER.length - 1;
      const next = Math.max(0, Math.min(max, i + delta));
      if (next !== i) playMove();
      return next;
    });
  }, []);

  // Activate the focused corner icon. Cuts through to the global hook
  // CornerIcons published when it mounted — keeps the action wiring
  // colocated with the rendering of the icon.
  function fireFocusedIcon() {
    if (cornerFocus < 0) return;
    playSelect();
    (window as any).__gsCornerActivate?.(cornerFocus);
  }

  // ── Controller / input gating ────────────────────────────────────────
  // While a picker/overlay is up, the carousel must ignore d-pad/A so a
  // left-press in the picker doesn't also shift the cursor behind it.
  const devicesMenuOpen = overlay === "devices";
  const updateModalOpen = update !== null && update.available && !pickerOpen && !audioOpen && !btOpen && !volOpen;
  const overlayBlocks =
    overlay === "details" ||
    overlay === "backups" ||
    overlay === "settings" ||
    overlay === "power" ||
    overlay === "devices";
  const inputBlocked = overlayBlocks || pickerOpen || audioOpen || btOpen || volOpen || updateModalOpen;

  useControllerNav((dir) => {
    if (inputBlocked) return;
    if (cornerFocus >= 0) {
      // Focus is on the corner icon row.
      if (dir === "left")       moveCornerFocus(-1);
      else if (dir === "right") moveCornerFocus(+1);
      else if (dir === "down")  { playMove(); setCornerFocus(-1); }
      // up while already on the icons = no-op (nowhere to go).
      return;
    }
    // Focus is on the carousel.
    if (dir === "left")        moveCursor(-1);
    else if (dir === "right")  moveCursor(+1);
    else if (dir === "up")     { playMove(); setCornerFocus(0); }
    // down on carousel = no-op (no row below).
  });

  useControllerButton((btn) => {
    if (pickerOpen || audioOpen || btOpen || volOpen || updateModalOpen) return;
    if (overlayBlocks) {
      // Every overlay handles its own B inside its component:
      // PowerMenu / DevicesMenu / GameDrawer / ShellSettingsPage /
      // ShellBackupsView / ShellUpdateModal. Leave them alone.
      return;
    }
    if (btn === "a") {
      if (cornerFocus >= 0) { fireFocusedIcon(); return; }
      if (active) doLaunch(active);
    } else if (btn === "y") {
      playSelect();
      setOverlay("settings");
    } else if (btn === "x" && active) {
      playSelect();
      setOverlay("details");
    } else if (btn === "start") {
      playSelect();
      setOverlay("power");
    } else if (btn === "back") {
      playSelect();
      setOverlay("devices");
    } else if (btn === "b") {
      // B on the carousel does nothing (no overlay to close). But if
      // the user happens to be focused on the icon row, B drops focus
      // back to the carousel — same shape as down.
      if (cornerFocus >= 0) { playBack(); setCornerFocus(-1); }
    }
  });

  // ── Keyboard ────────────────────────────────────────────────────────
  // Arrow keys mirror d-pad, Enter mirrors A, Escape mirrors B,
  // i=Y (info), p=Start (power), d=Back (devices). For users without
  // a controller. Inputs in overlays are NOT hijacked.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const target = e.target as HTMLElement | null;
      if (target && (target.tagName === "INPUT" || target.tagName === "TEXTAREA" || target.isContentEditable)) {
        return;
      }
      if (pickerOpen || audioOpen || btOpen || volOpen || updateModalOpen) return;
      if (overlayBlocks) {
        if (e.key === "Escape") { e.preventDefault(); playBack(); setOverlay("none"); }
        return;
      }
      if (e.key === "ArrowLeft")       { e.preventDefault();
        if (cornerFocus >= 0) moveCornerFocus(-1); else moveCursor(-1);
      } else if (e.key === "ArrowRight") { e.preventDefault();
        if (cornerFocus >= 0) moveCornerFocus(+1); else moveCursor(+1);
      } else if (e.key === "ArrowUp")   { e.preventDefault();
        if (cornerFocus < 0) { playMove(); setCornerFocus(0); }
      } else if (e.key === "ArrowDown") { e.preventDefault();
        if (cornerFocus >= 0) { playMove(); setCornerFocus(-1); }
      } else if (e.key === "Enter") { e.preventDefault();
        if (cornerFocus >= 0) fireFocusedIcon();
        else if (active) doLaunch(active);
      } else if (e.key.toLowerCase() === "i" && active) {
        e.preventDefault(); playSelect(); setOverlay("details");
      } else if (e.key.toLowerCase() === "p") {
        e.preventDefault(); playSelect(); setOverlay("power");
      } else if (e.key.toLowerCase() === "d") {
        e.preventDefault(); playSelect(); setOverlay("devices");
      } else if (e.key === ",") {
        // Common "settings" shortcut.
        e.preventDefault(); playSelect(); setOverlay("settings");
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [overlayBlocks, active, moveCursor, moveCornerFocus, cornerFocus, pickerOpen, audioOpen, updateModalOpen]);

  // ── Mouse wheel ─────────────────────────────────────────────────────
  const wheelLockRef = useRef(0);
  useEffect(() => {
    const onWheel = (e: WheelEvent) => {
      if (inputBlocked) return;
      const now = Date.now();
      if (now - wheelLockRef.current < 150) return;
      const d = Math.abs(e.deltaX) > Math.abs(e.deltaY) ? e.deltaX : e.deltaY;
      if (d === 0) return;
      wheelLockRef.current = now;
      moveCursor(d > 0 ? +1 : -1);
    };
    window.addEventListener("wheel", onWheel, { passive: true });
    return () => window.removeEventListener("wheel", onWheel);
  }, [inputBlocked, moveCursor]);

  async function doLaunch(g: GameView) {
    playSelect();
    const inst = g.installations[0];
    if (!inst) {
      api.Toast("error", "У игры нет установок");
      return;
    }
    try {
      await api.LaunchGame(g.game.id, inst.id);
      api.MinimizeSelf();
    } catch (e) {
      api.Toast("error", "Не удалось запустить: " + String(e));
    }
  }

  // Bring the shell back when a game session ends.
  useEffect(() => {
    const off = EventsOn("playtime:changed", (p: any) => {
      if (p && p.endedAt) api.RestoreSelf();
    });
    return () => { try { (off as any)?.(); } catch {} };
  }, []);

  // ── Ambient menu drone — see sound.ts for the lifecycle rationale. ───
  useEffect(() => {
    let stopped = false;
    const start = () => {
      if (stopped) return;
      if (document.hidden) return;
      if (getSoundPack() === "off") return;
      startAmbient();
    };
    const onVis = () => {
      if (document.hidden) stopAmbient();
      else start();
    };
    const onGesture = () => {
      start();
      window.removeEventListener("pointerdown", onGesture);
      window.removeEventListener("keydown", onGesture);
      window.removeEventListener("wheel", onGesture);
    };
    const unsubPack = subscribeSoundPack((p) => {
      if (p === "off") stopAmbient(); else start();
    });
    document.addEventListener("visibilitychange", onVis);
    window.addEventListener("pointerdown", onGesture);
    window.addEventListener("keydown", onGesture);
    window.addEventListener("wheel", onGesture);
    start();
    return () => {
      stopped = true;
      unsubPack();
      document.removeEventListener("visibilitychange", onVis);
      window.removeEventListener("pointerdown", onGesture);
      window.removeEventListener("keydown", onGesture);
      window.removeEventListener("wheel", onGesture);
      stopAmbient();
    };
  }, []);

  // Settings as a page renders OVER the carousel — the Settings page
  // owns the screen while overlay === "settings", with the animated
  // blob background instead of the active game's art (per the user
  // brief: "с нашим дефолтным анимированным фоном").
  if (overlay === "settings") {
    return (
      <ShellSettingsPage
        onClose={() => { playBack(); setOverlay("none"); }}
        onOpenMonitorPicker={() => openPicker()}
        onOpenAudioPicker={() => setAudioOpen(true)}
        onOpenBluetoothPicker={() => setBtOpen(true)}
      />
    );
  }

  return (
    <div className="fixed inset-0 overflow-hidden text-gray-100">
      <ShellBackground game={active} />

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
            <span><kbd className="rounded bg-white/10 px-1.5 py-0.5">X</kbd> подробнее</span>
            <span><kbd className="rounded bg-white/10 px-1.5 py-0.5">Y</kbd> настройки</span>
            <span><kbd className="rounded bg-white/10 px-1.5 py-0.5">Back</kbd> устройства</span>
            <span><kbd className="rounded bg-white/10 px-1.5 py-0.5">Start</kbd> питание</span>
          </span>
        )}
      </div>

      <CornerIcons
        onPickDevices={() => { playSelect(); setOverlay("devices"); }}
        onPower={() => { playSelect(); setOverlay("power"); }}
        onSettings={() => { playSelect(); setOverlay("settings"); }}
        onBackups={() => { playSelect(); setOverlay("backups"); }}
        focused={cornerFocus}
        onFocusChange={(i) => setCornerFocus(i)}
      />

      <HeroPanel
        game={active}
        onLaunch={() => active && doLaunch(active)}
        onDetails={() => { playSelect(); setOverlay("details"); }}
      />
      <GameCarousel
        games={sorted}
        activeIdx={activeIdx}
        onSelect={(i) => { if (i !== activeIdx) playMove(); setActiveIdx(i); setCornerFocus(-1); }}
      />

      {overlay === "details" && active && (
        <GameDrawer
          view={active}
          onClose={() => { playBack(); setOverlay("none"); }}
          onChanged={refresh}
        />
      )}
      {overlay === "backups" && (
        <ShellBackupsView
          games={games}
          onClose={() => { playBack(); setOverlay("none"); }}
        />
      )}

      {pickPrep && (
        <MonitorPicker
          prep={pickPrep}
          onDone={(chosenId) => {
            setPickPrep(null);
            if (chosenId) {
              committedRef.current = chosenId;
              lastCommitRef.current = Date.now();
            }
            if (audioAutoChainRef.current) {
              audioAutoChainRef.current = false;
              setAudioOpen(true);
            }
          }}
        />
      )}

      {audioOpen && <AudioPicker onDone={() => setAudioOpen(false)} />}

      {overlay === "power" && (
        <PowerMenu
          onClose={() => setOverlay("none")}
          onArmSuppress={armSuppress}
          onExit={async () => {
            try { await api.RestoreMonitorConfig(); } catch (e) { console.warn("restore monitors", e); }
            try { localStorage.removeItem("gs:soleMonitorId"); } catch {}
            api.QuitApp();
          }}
        />
      )}

      {overlay === "devices" && (
        <DevicesMenu
          onClose={() => setOverlay("none")}
          onPickMonitor={() => openPicker()}
          onPickAudio={() => setAudioOpen(true)}
          onPickBluetooth={() => setBtOpen(true)}
          onPickVolume={() => setVolOpen(true)}
        />
      )}

      {btOpen && <BluetoothPicker onDone={() => setBtOpen(false)} />}
      {volOpen && <VolumePicker onDone={() => setVolOpen(false)} />}

      {updateModalOpen && update && (
        <ShellUpdateModal info={update} onDismiss={onDismissUpdate} />
      )}
    </div>
  );
}
