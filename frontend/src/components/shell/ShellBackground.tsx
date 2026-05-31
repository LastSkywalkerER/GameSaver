// Immersive shell-mode background.
//
// Three-tier rendering, by priority:
//   1. Animated hero (webm/mp4) for the active game, autoplay+muted+loop —
//      same idea as Steam Big Picture / Apple TV's app pages.
//   2. Static hero (heroPath) or cover (coverPath) of the active game, scaled
//      to cover the whole screen.
//   3. Drifting purple/pink blobs (the v0.5 default) — only when there's no
//      game selected yet (empty library / scan in progress).
//
// On top of the art we lay a soft darkening overlay so the foreground UI
// (hero panel, carousel) stays legible. The overlay is intentionally
// LIGHTER than the v0.5 version — the user asked for the art to be more
// visible. A vignette keeps the corners calm so text in the hero panel
// still reads against a busy background.
//
// Crossfade: rendering the art with `key={game.id}` triggers the
// `animate-hero-fade` keyframe on every game switch, so the new image
// fades in over the old. The previous frame stays mounted for the
// duration of the CSS animation, no flicker.

import { useEffect, useRef, useState } from "react";
import { coverUrl, videoUrl, type GameView } from "../../api";

export function ShellBackground({ game }: { game: GameView | null }) {
  const videoSrc = videoUrl(game?.game.videoPath);
  const imgSrc   = coverUrl(game?.game.heroPath) ?? coverUrl(game?.game.coverPath);

  // Track whether the video actually started — until it does we keep the
  // still image behind it so we never show a blank black frame while the
  // browser is parsing the webm. Once playback emits 'playing' we fade
  // the still out.
  const [videoReady, setVideoReady] = useState(false);
  const videoRef = useRef<HTMLVideoElement | null>(null);

  // Reset readiness whenever the source changes — otherwise switching from
  // a video-having game to a video-less one leaves videoReady=true and
  // hides the still that's now the only thing we have.
  useEffect(() => { setVideoReady(false); }, [videoSrc, game?.game.id]);

  return (
    <>
      {/* Base gradient — deep navy/purple. Always present, peeks through
          the art at the edges (since object-cover may letterbox slightly
          on extreme aspect ratios) and provides the colour for the
          gradient-blob fallback below. */}
      <div className="absolute inset-0 bg-gradient-to-br from-[#0a0b1e] via-[#13153a] to-[#1c0e2e]" />

      {/* Fallback drifting blobs — only when we have NO art at all. */}
      {!videoSrc && !imgSrc && (
        <div className="absolute inset-0 overflow-hidden opacity-50">
          <div className="absolute h-[70vh] w-[70vh] rounded-full bg-accent/40 blur-[140px] animate-blob-a" />
          <div className="absolute h-[55vh] w-[55vh] rounded-full bg-blue-500/30 blur-[120px] animate-blob-b" />
          <div className="absolute h-[45vh] w-[45vh] rounded-full bg-pink-500/25 blur-[120px] animate-blob-c" />
        </div>
      )}

      {/* Still image — shown whenever we have one, AND as a placeholder
          underneath the video until 'playing' fires. */}
      {imgSrc && (
        <img
          key={"img-" + game?.game.id}
          src={imgSrc}
          alt=""
          aria-hidden
          className={
            "animate-hero-fade absolute inset-0 h-full w-full object-cover transition-opacity duration-500 " +
            (videoSrc && videoReady ? "opacity-0" : "opacity-100")
          }
        />
      )}

      {/* Animated hero — autoplay muted loop so Chromium lets it through
          autoplay policy. `playsInline` keeps it from going fullscreen on
          mobile WebViews (not that we ship to mobile, but it's the safe
          default). preload="auto" so we don't see a black flash while
          metadata loads. */}
      {videoSrc && (
        <video
          key={"vid-" + game?.game.id}
          ref={videoRef}
          src={videoSrc}
          autoPlay
          muted
          loop
          playsInline
          preload="auto"
          onPlaying={() => setVideoReady(true)}
          onError={() => setVideoReady(false)}
          className={
            "absolute inset-0 h-full w-full object-cover transition-opacity duration-700 " +
            (videoReady ? "opacity-100" : "opacity-0")
          }
        />
      )}

      {/* Left-side fade — full screen height, dies out around the middle.
          This used to live in HeroPanel but ended at h-[55vh] there,
          which left a visible horizontal seam where the darkening
          stopped. Drawing it here as a tall slab keeps the title
          readable AND lets the carousel area underneath stay tinted on
          the left without any edge. */}
      <div className="absolute inset-y-0 left-0 w-[55%] bg-gradient-to-r from-black/70 via-black/25 to-transparent" />

      {/* Darkening overlay — kept lighter than v0.5 so the art reads
          through. A vertical gradient bottoms out at ~45% so the
          carousel still has its dark plate; the top stays clearer so
          the hero art's actual subject (usually centred upper-mid in
          Steam art) shows. */}
      <div className="absolute inset-0 bg-gradient-to-b from-black/15 via-black/25 to-black/55" />
      {/* Subtle vignette so the corners don't compete with foreground UI. */}
      <div className="absolute inset-0 bg-[radial-gradient(ellipse_at_center,transparent_55%,rgba(0,0,0,0.55)_100%)]" />
    </>
  );
}
