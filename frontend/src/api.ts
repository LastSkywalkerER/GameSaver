// Thin wrapper around generated Wails bindings.
// We re-export them and also add typed shapes used across the UI.

import * as App from "../wailsjs/go/main/App";
import { EventsOn } from "../wailsjs/runtime/runtime";

export const api = App;
export { EventsOn };

export type SourceKind =
  | "steam"
  | "gog"
  | "epic"
  | "ea"
  | "ubisoft"
  | "xbox"
  | "battlenet"
  | "riot"
  | "minecraft"
  | "microsoftStore"
  | "standalone"
  | "pirate"
  | "repack"
  | "unknown";

export type Trigger = "manual" | "auto" | "preRestore" | "preMigrate";

export interface Game {
  id: string;
  name: string;
  slug: string;
  igdbId?: number;
  steamAppId?: number;
  coverPath?: string;
  heroPath?: string;
  iconPath?: string;
  genres?: string;
  releaseYear?: number;
  hidden?: boolean;
}

export interface Installation {
  id: string;
  gameId: string;
  source: SourceKind;
  sourceAppId?: string;
  rootPath: string;
  exePath: string;
  sizeBytes?: number;
  lastSeenAt: number;
  launchUri?: string;
}

export interface SaveLocation {
  id: string;
  gameId: string;
  path: string;
  kind: string;
  sourceHint?: string;
  sizeBytes: number;
  fileCount: number;
  mtime: number;
  watchEnabled: boolean;
  isJunction: boolean;
  junctionTarget?: string;
}

export interface Snapshot {
  id: string;
  gameId: string;
  locationId: string;
  archivePath: string;
  createdAt: number;
  trigger: Trigger;
  totalBytes: number;
  compressedBytes: number;
  fileCount: number;
  encrypted: boolean;
  contentHash: string;
  gameVersion?: string;
  note?: string;
}

export interface GameView {
  game: Game;
  installations: Installation[];
  saveLocations: SaveLocation[];
  snapshots: Snapshot[];
}

export interface ScanResult {
  gamesUpserted: number;
  installationsTotal: number;
  perSource: Record<string, number>;
  durationMs: number;
}

export interface AppConfig {
  backupRoot: string;
  steamGridDbKey?: string;
  language: string;
  theme: string;
  watcherEnabled: boolean;
  watcherDebounceMs: number;
  retentionKeepN: number;
  autostartTray: boolean;
}

export function coverUrl(name?: string): string | undefined {
  if (!name) return undefined;
  return "/covers/" + name;
}

export function formatBytes(n?: number): string {
  if (!n || n <= 0) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  let i = 0;
  let v = n;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return v.toFixed(v >= 10 || i === 0 ? 0 : 1) + " " + units[i];
}

export function formatDate(ts?: number): string {
  if (!ts) return "";
  return new Date(ts * 1000).toLocaleString();
}

export interface ManifestSearchResult {
  key: string;
  name: string;
  steamAppId?: number;
  fileCount: number;
}

export interface DeepScanResult {
  path: string;
  fileCount: number;
  sizeBytes: number;
  mtime: number;
  score: number;
  reason: string;
}

export interface ReverseScanResult {
  entriesChecked: number;
  entriesMatched: number;
  newGamesCreated: number;
  newLocationsAdded: number;
  durationMs: number;
}

export interface ReconcileResult {
  scannedManifests: number;
  importedSnapshots: number;
  alreadyKnown: number;
  createdGames: number;
  createdLocations: number;
  orphanZips: number;
  orphanManifests: number;
  deadSnapshots: number;
}
