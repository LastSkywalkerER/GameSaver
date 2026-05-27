// HeroPanel — the top portion of shell-mode UI. Shows the currently
// highlighted game's hero art / cover huge, with the title, quick stats,
// and a big Play button. The hero art crossfades whenever the active
// game changes.

import { useEffect, useState } from "react";
import { coverUrl, formatDuration, formatRelative, type GameView } from "../../api";

export function HeroPanel({
  game,
  onLaunch,
  onDetails,
}: {
  game: GameView | null;
  onLaunch: () => void;
  onDetails: () => void;
}) {
  // Track the image URL with a key so we get a fresh element each switch,
  // triggering the hero-fade-in animation.
  const heroSrc = game ? coverUrl(game.game.heroPath) ?? coverUrl(game.game.coverPath) : undefined;

  if (!game) {
    return (
      <div className="absolute inset-x-0 top-0 flex h-[55vh] items-center justify-center text-xl text-gray-400">
        Игр пока нет — запусти скан в обычном режиме
      </div>
    );
  }

  const g = game.game;
  const hasInstall = game.installations.length > 0;
  const hasBackup  = game.snapshots.length > 0;

  return (
    // Height intentionally shorter than carousel-top so there's breathing
    // room between the hero's bottom and the carousel tiles — otherwise
    // the edges line up and look like one solid cut.
    <div className="absolute inset-x-0 top-0 flex h-[55vh] items-end overflow-hidden">
      {heroSrc && (
        <img
          key={g.id}
          src={heroSrc}
          alt={g.name}
          className="animate-hero-fade absolute inset-0 h-full w-full object-cover opacity-60"
        />
      )}
      {/* Bottom-to-top + right-to-left dark gradient so text on the left
          stays readable regardless of cover content. The bottom-fade is
          tall (40%) so the hero blends into the background well above
          the carousel — no hard seam where the two halves meet. */}
      <div className="absolute inset-0 bg-gradient-to-r from-black/95 via-black/50 to-transparent" />
      <div className="absolute inset-0 bg-gradient-to-t from-[#0a0b1e] via-[#0a0b1e]/40 to-transparent" style={{ backgroundSize: "100% 100%" }} />

      {/* pb-20 lifts content up so the Play button sits comfortably away
          from the hero/carousel seam. */}
      <div className="relative z-10 max-w-[55%] p-16 pb-20">
        <div className="mb-3 flex items-center gap-3 text-sm uppercase tracking-widest text-accent">
          {game.installations.map((i) => i.source).filter((v, i, a) => a.indexOf(v) === i).join(" · ")}
        </div>
        <h1 className="text-7xl font-bold leading-tight tracking-tight drop-shadow-2xl">
          {g.name}
        </h1>
        <div className="mt-5 flex flex-wrap items-center gap-4 text-sm text-gray-300">
          {g.lastPlayedAt ? <span>▶ {formatRelative(g.lastPlayedAt)}</span> : null}
          {g.totalPlaySeconds ? <span>⏱ {formatDuration(g.totalPlaySeconds)}</span> : null}
          {hasBackup ? <span>⛁ {game.snapshots.length} бэкапов</span> : null}
        </div>
        <div className="mt-10 flex gap-4">
          <button
            onClick={onLaunch}
            disabled={!hasInstall}
            className="group flex items-center gap-3 rounded-2xl bg-accent px-10 py-5 text-xl font-bold text-white shadow-[0_10px_40px_rgba(124,92,255,0.45)] transition hover:scale-105 hover:bg-[#6a4ce0] disabled:cursor-not-allowed disabled:bg-zinc-700 disabled:text-zinc-400 disabled:shadow-none"
            title={hasInstall ? "A — запустить" : "Нет установок — нечего запускать"}
          >
            <span className="text-2xl">▶</span> Запустить
          </button>
          <button
            onClick={onDetails}
            className="rounded-2xl border border-white/20 bg-white/10 px-6 py-5 text-lg text-gray-100 backdrop-blur-sm transition hover:bg-white/15"
            title="Y — подробности (пути, бэкапы, сейвы)"
          >
            ⓘ Подробнее
          </button>
        </div>
      </div>
    </div>
  );
}
