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
  videoPath?: string;  // animated hero (webm/mp4) for shell background
  genres?: string;
  releaseYear?: number;
  hidden?: boolean;
  lastPlayedAt?: number;     // unix seconds; 0 = never
  totalPlaySeconds?: number; // lifetime total
}

export interface Installation {
  id: string;
  gameId: string;
  source: SourceKind;
  sourceAppId?: string;
  rootPath: string;
  exePath: string;
  sizeBytes?: number;             // exe size
  installDirSizeBytes?: number;   // recursive install-root size (lazy)
  installDirSizeAt?: number;      // unix ts of last measurement
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

// ─── Store libraries (#5) ──────────────────────────────────────────────────

export interface StoreAccount {
  id: string;
  store: SourceKind;
  externalId: string;
  displayName: string;
  avatarUrl?: string;
  addedAt: number;
  lastSyncAt?: number;
  lastSyncError?: string;
  enabled: boolean;
}

export interface OwnershipFact {
  store: SourceKind;
  accountId: string;
  accountName: string;
  storeAppId: string;
  owned: boolean;
  installed: boolean;
  playtimeMin?: number;
}

export interface LibraryCard {
  game: Game;
  ownership: OwnershipFact[];
  installations: Installation[];
  anyInstalled: boolean;
  imageUrl?: string;
}

export interface StoreLoginInfo {
  store: SourceKind;
  url: string;
  hint: string;
  interactive: boolean;
}

export interface StoreClientStatus {
  store: SourceKind;
  installed: boolean;
  openUri: string;
  downloadUrl: string;
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
  autoCheckUpdates?: boolean;
  skippedUpdateVersion?: string;
  lastUpdateCheckUnix?: number;
}

export function coverUrl(name?: string): string | undefined {
  if (!name) return undefined;
  return "/covers/" + name;
}

// Animated heroes live in the same coversDir, served by the same /covers/
// route — this is just a semantic alias so the call site reads as video.
export function videoUrl(name?: string): string | undefined {
  return coverUrl(name);
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

// formatDuration renders seconds as "2h 35m" / "12m 30s" / "47s".
export function formatDuration(seconds?: number): string {
  if (!seconds || seconds <= 0) return "0";
  const s = Math.floor(seconds);
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  const sec = s % 60;
  if (h > 0) return `${h}h ${m}m`;
  if (m > 0) return `${m}m ${sec}s`;
  return `${sec}s`;
}

// formatRelative renders "5 минут назад" / "3 дня назад" etc. Empty if !ts.
export function formatRelative(ts?: number): string {
  if (!ts) return "";
  const diff = Math.floor(Date.now() / 1000 - ts);
  if (diff < 60) return "только что";
  if (diff < 3600) return `${Math.floor(diff / 60)} мин назад`;
  if (diff < 86400) return `${Math.floor(diff / 3600)} ч назад`;
  if (diff < 86400 * 30) return `${Math.floor(diff / 86400)} дн назад`;
  if (diff < 86400 * 365) return `${Math.floor(diff / 86400 / 30)} мес назад`;
  return `${Math.floor(diff / 86400 / 365)} лет назад`;
}

export interface PlaySession {
  id: string;
  gameId: string;
  installationId?: string;
  startedAt: number;
  endedAt?: number;
  durationSeconds: number;
  source: string;
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

// ─── Nintendo Switch over MTP ──────────────────────────────────────────────

// Where a backup was captured from. Manifests written before this existed have
// no platform field and are treated as "windows" by the backend.
export type Platform = "windows" | "switch";

export interface SwitchStatus {
  connected: boolean;
  name: string;
  serial: string;
  error?: string;
}

// "profile" is a console user account (the common case). The rest are genuine
// save-data types that live at the same level of DBI's tree.
export type SwitchEntryKind = "profile" | "bcat" | "device" | "cache";

export interface SwitchEntry {
  name: string;
  objectId: string;
  kind: SwitchEntryKind;
  fileCount: number;
  sizeBytes: number;
}

export interface SwitchTitle {
  name: string;
  objectId: string;
  installed: boolean;
  entries: SwitchEntry[] | null;
}

export interface SwitchLibrary {
  device: { pnpId: string; friendlyName: string; serial: string };
  titles: SwitchTitle[] | null;
  scannedAt: number;
}

export interface SwitchBackup {
  title: string;
  profile: string;
  serial: string;
  platform: Platform;
  createdAt: number;
  fileCount: number;
  totalBytes: number;
  archivePath: string;
  sizeOnDisk: number;
  trigger: Trigger;
}

export interface SwitchBackupResult {
  title: string;
  profile: string;
  archivePath: string;
  fileCount: number;
  totalBytes: number;
  skipped: boolean;
  error?: string;
}

export interface SwitchSelection {
  title: string;
  profile: string;
}

export interface PlannedWrite {
  rel: string;
  size: number;
  replaces: boolean;
}

export interface UploadPlan {
  title: string;
  profile: string;
  writes: PlannedWrite[] | null;
  newFiles: number;
  replaced: number;
  totalBytes: number;
  untouched: string[] | null;
}

export interface SwitchDownloadResult {
  files: number;
  totalBytes: number;
}

export interface UpdateInfo {
  available: boolean;
  currentVersion: string;
  latestVersion: string;
  releaseUrl: string;
  releaseNotes?: string;
  assetUrl?: string;
  assetSize?: number;
  sha256?: string;
  publishedAt?: string;
}
