export namespace backup {
	
	export class ReconcileResult {
	    scannedManifests: number;
	    importedSnapshots: number;
	    alreadyKnown: number;
	    createdGames: number;
	    createdLocations: number;
	    orphanZips: number;
	    orphanManifests: number;
	    deadSnapshots: number;
	
	    static createFrom(source: any = {}) {
	        return new ReconcileResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.scannedManifests = source["scannedManifests"];
	        this.importedSnapshots = source["importedSnapshots"];
	        this.alreadyKnown = source["alreadyKnown"];
	        this.createdGames = source["createdGames"];
	        this.createdLocations = source["createdLocations"];
	        this.orphanZips = source["orphanZips"];
	        this.orphanManifests = source["orphanManifests"];
	        this.deadSnapshots = source["deadSnapshots"];
	    }
	}

}

export namespace config {
	
	export class Config {
	    backupRoot: string;
	    steamGridDbKey?: string;
	    language: string;
	    theme: string;
	    extraScanRoots?: string[];
	    excludeScanRoots?: string[];
	    watcherEnabled: boolean;
	    watcherDebounceMs: number;
	    retentionKeepN: number;
	    autostartTray: boolean;
	    autoCheckUpdates: boolean;
	    skippedUpdateVersion?: string;
	    lastUpdateCheckUnix?: number;
	
	    static createFrom(source: any = {}) {
	        return new Config(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.backupRoot = source["backupRoot"];
	        this.steamGridDbKey = source["steamGridDbKey"];
	        this.language = source["language"];
	        this.theme = source["theme"];
	        this.extraScanRoots = source["extraScanRoots"];
	        this.excludeScanRoots = source["excludeScanRoots"];
	        this.watcherEnabled = source["watcherEnabled"];
	        this.watcherDebounceMs = source["watcherDebounceMs"];
	        this.retentionKeepN = source["retentionKeepN"];
	        this.autostartTray = source["autostartTray"];
	        this.autoCheckUpdates = source["autoCheckUpdates"];
	        this.skippedUpdateVersion = source["skippedUpdateVersion"];
	        this.lastUpdateCheckUnix = source["lastUpdateCheckUnix"];
	    }
	}

}

export namespace display {
	
	export class Monitor {
	    id: string;
	    name: string;
	    width: number;
	    height: number;
	    positionX: number;
	    positionY: number;
	    isPrimary: boolean;
	    isEnabled: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Monitor(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.width = source["width"];
	        this.height = source["height"];
	        this.positionX = source["positionX"];
	        this.positionY = source["positionY"];
	        this.isPrimary = source["isPrimary"];
	        this.isEnabled = source["isEnabled"];
	    }
	}

}

export namespace domain {
	
	export class Game {
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
	    hidden: boolean;
	    createdAt: number;
	    updatedAt: number;
	    lastPlayedAt?: number;
	    totalPlaySeconds?: number;
	
	    static createFrom(source: any = {}) {
	        return new Game(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.slug = source["slug"];
	        this.igdbId = source["igdbId"];
	        this.steamAppId = source["steamAppId"];
	        this.coverPath = source["coverPath"];
	        this.heroPath = source["heroPath"];
	        this.iconPath = source["iconPath"];
	        this.genres = source["genres"];
	        this.releaseYear = source["releaseYear"];
	        this.hidden = source["hidden"];
	        this.createdAt = source["createdAt"];
	        this.updatedAt = source["updatedAt"];
	        this.lastPlayedAt = source["lastPlayedAt"];
	        this.totalPlaySeconds = source["totalPlaySeconds"];
	    }
	}
	export class Snapshot {
	    id: string;
	    gameId: string;
	    locationId: string;
	    archivePath: string;
	    createdAt: number;
	    trigger: string;
	    totalBytes: number;
	    compressedBytes: number;
	    fileCount: number;
	    encrypted: boolean;
	    contentHash: string;
	    gameVersion?: string;
	    note?: string;
	
	    static createFrom(source: any = {}) {
	        return new Snapshot(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.gameId = source["gameId"];
	        this.locationId = source["locationId"];
	        this.archivePath = source["archivePath"];
	        this.createdAt = source["createdAt"];
	        this.trigger = source["trigger"];
	        this.totalBytes = source["totalBytes"];
	        this.compressedBytes = source["compressedBytes"];
	        this.fileCount = source["fileCount"];
	        this.encrypted = source["encrypted"];
	        this.contentHash = source["contentHash"];
	        this.gameVersion = source["gameVersion"];
	        this.note = source["note"];
	    }
	}
	export class SaveLocation {
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
	
	    static createFrom(source: any = {}) {
	        return new SaveLocation(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.gameId = source["gameId"];
	        this.path = source["path"];
	        this.kind = source["kind"];
	        this.sourceHint = source["sourceHint"];
	        this.sizeBytes = source["sizeBytes"];
	        this.fileCount = source["fileCount"];
	        this.mtime = source["mtime"];
	        this.watchEnabled = source["watchEnabled"];
	        this.isJunction = source["isJunction"];
	        this.junctionTarget = source["junctionTarget"];
	    }
	}
	export class Installation {
	    id: string;
	    gameId: string;
	    source: string;
	    sourceAppId?: string;
	    rootPath: string;
	    exePath: string;
	    sizeBytes?: number;
	    installDirSizeBytes?: number;
	    installDirSizeAt?: number;
	    lastSeenAt: number;
	    launchUri?: string;
	
	    static createFrom(source: any = {}) {
	        return new Installation(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.gameId = source["gameId"];
	        this.source = source["source"];
	        this.sourceAppId = source["sourceAppId"];
	        this.rootPath = source["rootPath"];
	        this.exePath = source["exePath"];
	        this.sizeBytes = source["sizeBytes"];
	        this.installDirSizeBytes = source["installDirSizeBytes"];
	        this.installDirSizeAt = source["installDirSizeAt"];
	        this.lastSeenAt = source["lastSeenAt"];
	        this.launchUri = source["launchUri"];
	    }
	}
	export class GameView {
	    game?: Game;
	    installations: Installation[];
	    saveLocations: SaveLocation[];
	    snapshots: Snapshot[];
	
	    static createFrom(source: any = {}) {
	        return new GameView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.game = this.convertValues(source["game"], Game);
	        this.installations = this.convertValues(source["installations"], Installation);
	        this.saveLocations = this.convertValues(source["saveLocations"], SaveLocation);
	        this.snapshots = this.convertValues(source["snapshots"], Snapshot);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class PlaySession {
	    id: string;
	    gameId: string;
	    installationId?: string;
	    startedAt: number;
	    endedAt?: number;
	    durationSeconds: number;
	    source: string;
	
	    static createFrom(source: any = {}) {
	        return new PlaySession(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.gameId = source["gameId"];
	        this.installationId = source["installationId"];
	        this.startedAt = source["startedAt"];
	        this.endedAt = source["endedAt"];
	        this.durationSeconds = source["durationSeconds"];
	        this.source = source["source"];
	    }
	}
	

}

export namespace main {
	
	export class AutoLoginStatus {
	    checkboxHidden: boolean;
	
	    static createFrom(source: any = {}) {
	        return new AutoLoginStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.checkboxHidden = source["checkboxHidden"];
	    }
	}
	export class ShellModeStatus {
	    watchdogPresent: boolean;
	    registered: boolean;
	    runningAsShell: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ShellModeStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.watchdogPresent = source["watchdogPresent"];
	        this.registered = source["registered"];
	        this.runningAsShell = source["runningAsShell"];
	    }
	}

}

export namespace match {
	
	export class DeepScanResult {
	    path: string;
	    fileCount: number;
	    sizeBytes: number;
	    mtime: number;
	    score: number;
	    reason: string;
	
	    static createFrom(source: any = {}) {
	        return new DeepScanResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.fileCount = source["fileCount"];
	        this.sizeBytes = source["sizeBytes"];
	        this.mtime = source["mtime"];
	        this.score = source["score"];
	        this.reason = source["reason"];
	    }
	}
	export class ManifestSearchResult {
	    key: string;
	    name: string;
	    steamAppId?: number;
	    fileCount: number;
	
	    static createFrom(source: any = {}) {
	        return new ManifestSearchResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.name = source["name"];
	        this.steamAppId = source["steamAppId"];
	        this.fileCount = source["fileCount"];
	    }
	}
	export class ReverseScanResult {
	    entriesChecked: number;
	    entriesMatched: number;
	    newGamesCreated: number;
	    newLocationsAdded: number;
	    durationMs: number;
	
	    static createFrom(source: any = {}) {
	        return new ReverseScanResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.entriesChecked = source["entriesChecked"];
	        this.entriesMatched = source["entriesMatched"];
	        this.newGamesCreated = source["newGamesCreated"];
	        this.newLocationsAdded = source["newLocationsAdded"];
	        this.durationMs = source["durationMs"];
	    }
	}

}

export namespace pipeline {
	
	export class Result {
	    gamesUpserted: number;
	    installationsTotal: number;
	    perSource: Record<string, number>;
	    durationMs: number;
	
	    static createFrom(source: any = {}) {
	        return new Result(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.gamesUpserted = source["gamesUpserted"];
	        this.installationsTotal = source["installationsTotal"];
	        this.perSource = source["perSource"];
	        this.durationMs = source["durationMs"];
	    }
	}

}

export namespace updater {
	
	export class UpdateInfo {
	    available: boolean;
	    currentVersion: string;
	    latestVersion: string;
	    releaseUrl: string;
	    releaseNotes?: string;
	    assetUrl?: string;
	    assetSize?: number;
	    sha256?: string;
	    publishedAt?: string;
	
	    static createFrom(source: any = {}) {
	        return new UpdateInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.available = source["available"];
	        this.currentVersion = source["currentVersion"];
	        this.latestVersion = source["latestVersion"];
	        this.releaseUrl = source["releaseUrl"];
	        this.releaseNotes = source["releaseNotes"];
	        this.assetUrl = source["assetUrl"];
	        this.assetSize = source["assetSize"];
	        this.sha256 = source["sha256"];
	        this.publishedAt = source["publishedAt"];
	    }
	}

}

