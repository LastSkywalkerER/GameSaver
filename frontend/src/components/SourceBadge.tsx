import clsx from "clsx";
import type { SourceKind } from "../api";

const LABELS: Partial<Record<SourceKind, { label: string; cls: string }>> = {
  steam:      { label: "Steam",    cls: "bg-[#1b2838] text-[#66c0f4] border-[#2a475e]" },
  gog:        { label: "GOG",      cls: "bg-[#2b1530] text-[#e94d8b] border-[#5c2657]" },
  epic:       { label: "Epic",     cls: "bg-[#0d1119] text-[#f4f4f5] border-[#2a2d39]" },
  ea:         { label: "EA",       cls: "bg-[#102a1e] text-[#4ade80] border-[#16432c]" },
  ubisoft:    { label: "Ubisoft",  cls: "bg-[#0a1726] text-[#7dd3fc] border-[#1e3a5f]" },
  xbox:       { label: "Xbox",     cls: "bg-[#0a1a0e] text-[#a3e635] border-[#1b3a1b]" },
  battlenet:  { label: "Battle.net", cls: "bg-[#0b1530] text-[#93c5fd] border-[#1e3a5f]" },
  riot:       { label: "Riot",     cls: "bg-[#2b1212] text-[#fda4af] border-[#5b1f1f]" },
  minecraft:  { label: "Minecraft", cls: "bg-[#102a1e] text-[#86efac] border-[#16432c]" },
  microsoftStore: { label: "MS Store", cls: "bg-[#0a1a26] text-[#7dd3fc] border-[#1e3a5f]" },
  standalone: { label: "Standalone", cls: "bg-[#1f1f28] text-[#cbd5e1] border-[#2a2d39]" },
  pirate:     { label: "Pirate",   cls: "bg-[#2b1212] text-[#fca5a5] border-[#5b1f1f]" },
  repack:     { label: "Repack",   cls: "bg-[#2b1212] text-[#fca5a5] border-[#5b1f1f]" },
};

export function SourceBadge({ source }: { source: SourceKind }) {
  const meta = LABELS[source] ?? { label: source, cls: "bg-card text-gray-300 border-border" };
  return (
    <span className={clsx("chip border", meta.cls)}>{meta.label}</span>
  );
}
