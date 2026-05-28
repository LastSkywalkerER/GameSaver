// Top-right corner toolbar in shell mode. Tiny circular icons for
// Settings / Backups / Exit-shell — no labels, hover tooltip only,
// since the goal is to keep the foreground minimal and PS-like.

import { api } from "../../api";
import { isNavSoundEnabled, playSelect, setNavSoundEnabled } from "../../sound";
import { useState } from "react";

export function CornerIcons({
  onSwitchMonitor,
  onPower,
  onSettings,
  onBackups,
  onExit,
}: {
  onSwitchMonitor: () => void;
  onPower: () => void;
  onSettings: () => void;
  onBackups: () => void;
  onExit: () => void;
}) {
  // Sound toggle lives here so users can mute the chip-tunes without
  // diving into Settings. Initialised from localStorage.
  const [sound, setSound] = useState<boolean>(isNavSoundEnabled());
  const toggleSound = () => {
    const v = !sound;
    setNavSoundEnabled(v);
    setSound(v);
    if (v) playSelect();
  };

  return (
    <div className="absolute right-6 top-6 z-20 flex gap-3">
      <IconButton title={sound ? "Звук вкл — выключить" : "Звук выкл — включить"} onClick={toggleSound}>
        {sound ? "🔊" : "🔇"}
      </IconButton>
      <IconButton title="Сменить монитор" onClick={onSwitchMonitor}>🖥</IconButton>
      <IconButton title="Бэкапы" onClick={onBackups}>⛁</IconButton>
      <IconButton title="Настройки" onClick={onSettings}>⚙</IconButton>
      <IconButton title="Питание (Lock / Sleep / Exit) — X на геймпаде" onClick={onPower}>⏻</IconButton>
      <IconButton
        title="Выйти из shell-режима (вернёт Explorer)"
        onClick={async () => {
          await api.DisableShellMode().catch(() => {});
          onExit();
        }}
      >
        🛑
      </IconButton>
    </div>
  );
}

function IconButton({
  title,
  onClick,
  children,
}: {
  title: string;
  onClick: () => void;
  children: React.ReactNode;
}) {
  return (
    <button
      title={title}
      onClick={onClick}
      className="grid h-12 w-12 place-items-center rounded-full border border-white/10 bg-white/5 text-xl text-gray-200 backdrop-blur-md transition hover:scale-110 hover:bg-white/15"
    >
      {children}
    </button>
  );
}
