// Immersive animated background for shell mode. Three slow-drifting blurred
// blobs over a deep-purple gradient — gives the screen ambient motion the
// way the PS5 home screen does, without distracting from the foreground.

export function ShellBackground() {
  return (
    <>
      {/* Base gradient — fixed deep purple-to-navy. */}
      <div className="absolute inset-0 bg-gradient-to-br from-[#0a0b1e] via-[#13153a] to-[#1c0e2e]" />
      {/* Drifting colour blobs — each its own keyframe period. */}
      <div className="absolute inset-0 overflow-hidden opacity-50">
        <div className="absolute h-[70vh] w-[70vh] rounded-full bg-accent/40 blur-[140px] animate-blob-a" />
        <div className="absolute h-[55vh] w-[55vh] rounded-full bg-blue-500/30 blur-[120px] animate-blob-b" />
        <div className="absolute h-[45vh] w-[45vh] rounded-full bg-pink-500/25 blur-[120px] animate-blob-c" />
      </div>
      {/* Subtle vignette so the corners don't compete with hero art. */}
      <div className="absolute inset-0 bg-[radial-gradient(ellipse_at_center,transparent_40%,rgba(0,0,0,0.5)_100%)]" />
    </>
  );
}
