// Top-right corner toolbar in shell mode. Tiny circular icons for
// device-chooser / backups / settings / power — no labels, hover
// tooltip only, since the goal is to keep the foreground minimal and
// PS-like.
//
// v0.10.0 layout changes:
//   - 🖥 (monitor) + 🎧 (audio) collapsed into one 🎛 (devices) icon
//     that opens a chooser modal (DevicesMenu) → monitor / audio / BT.
//     Frees up one slot and matches the new Back-button binding.
//   - 🛑 (exit shell) removed — Exit now lives inside the power menu,
//     where it belongs alongside Lock / Sleep / Reboot. A user can no
//     longer fat-finger the kiosk-disable button by accident.
//
// Up/Down on the d-pad rotates focus between this row and the
// carousel below (handled by ShellApp). When focus IS in this row,
// Left/Right walks the icons.

import { useEffect, useRef } from "react";
import { isNavSoundEnabled, playSelect, setNavSoundEnabled } from "../../sound";
import { useState } from "react";

export type CornerIconKey =
  | "sound" | "devices" | "backups" | "settings" | "power";

export const CORNER_ICON_ORDER: CornerIconKey[] = [
  "sound", "devices", "backups", "settings", "power",
];

export function CornerIcons({
  onPickDevices,
  onPower,
  onSettings,
  onBackups,
  focused,
  onFocusChange,
}: {
  onPickDevices: () => void;
  onPower: () => void;
  onSettings: () => void;
  onBackups: () => void;
  /** When the carousel hands focus up to this row, the active icon
   *  index lives here so left/right walks across them. -1 = not
   *  focused (the row is just visible but not the input target). */
  focused: number;
  onFocusChange: (i: number) => void;
}) {
  const [sound, setSound] = useState<boolean>(isNavSoundEnabled());
  const toggleSound = () => {
    const v = !sound;
    setNavSoundEnabled(v);
    setSound(v);
    if (v) playSelect();
  };

  // Map each ordered icon to its action so ShellApp can fire the
  // focused one on A press — same data-driven shape PowerMenu uses.
  const ACTIONS: Record<CornerIconKey, () => void> = {
    sound: toggleSound,
    devices: onPickDevices,
    backups: onBackups,
    settings: onSettings,
    power: onPower,
  };

  // Expose activation via a ref so ShellApp can fire the focused one
  // without re-rendering this component. (A direct prop would re-bind
  // every render — fine, but the ref avoids that churn.)
  const actRef = useRef(ACTIONS);
  actRef.current = ACTIONS;
  useEffect(() => {
    (window as any).__gsCornerActivate = (i: number) => {
      const k = CORNER_ICON_ORDER[i]; if (!k) return;
      actRef.current[k]();
    };
    return () => { delete (window as any).__gsCornerActivate; };
  }, []);

  return (
    <div className="absolute right-6 top-6 z-20 flex gap-3">
      <IconButton
        i={0}
        focused={focused}
        title={sound ? "Звук вкл — выключить" : "Звук выкл — включить"}
        onClick={toggleSound}
        onFocus={() => onFocusChange(0)}
      >{sound ? "🔊" : "🔇"}</IconButton>
      <IconButton
        i={1}
        focused={focused}
        title="Устройства (монитор / аудио / Bluetooth)"
        onClick={onPickDevices}
        onFocus={() => onFocusChange(1)}
      >🎛</IconButton>
      <IconButton
        i={2}
        focused={focused}
        title="Бэкапы"
        onClick={onBackups}
        onFocus={() => onFocusChange(2)}
      >⛁</IconButton>
      <IconButton
        i={3}
        focused={focused}
        title="Настройки (Y на геймпаде)"
        onClick={onSettings}
        onFocus={() => onFocusChange(3)}
      >⚙</IconButton>
      <IconButton
        i={4}
        focused={focused}
        title="Питание (Lock / Sleep / Reboot / Exit) — X / Start на геймпаде"
        onClick={onPower}
        onFocus={() => onFocusChange(4)}
      >⏻</IconButton>
    </div>
  );
}

function IconButton({
  i, focused, title, onClick, onFocus, children,
}: {
  i: number;
  focused: number;
  title: string;
  onClick: () => void;
  onFocus: () => void;
  children: React.ReactNode;
}) {
  const isFocused = i === focused;
  return (
    <button
      title={title}
      onClick={onClick}
      onMouseEnter={onFocus}
      className={
        "grid h-12 w-12 place-items-center rounded-full border text-xl text-gray-200 backdrop-blur-md transition hover:scale-110 " +
        (isFocused
          ? "border-accent bg-accent/30 scale-110 shadow-[0_10px_30px_rgba(124,92,255,0.45)]"
          : "border-white/10 bg-white/5 hover:bg-white/15")
      }
    >
      {children}
    </button>
  );
}
