import Database from "better-sqlite3";
import path from "path";
import fs from "fs";
import crypto from "crypto";

const DEFAULT_MANIFEST_URL =
  "https://raw.githubusercontent.com/MatthewTran22/LoLOverlay-Data/main/manifest.json";

interface Manifest {
  version: string;
  data_url: string;
  data_sha256: string;
  updated_at: string;
  force_reset: boolean;
}

interface DataExport {
  patch: string;
  generatedAt: string;
  championStats: ChampionStatJSON[];
  championItems: ChampionItemJSON[];
  championItemSlots: ChampionItemSlotJSON[];
  championMatchups: ChampionMatchupJSON[];
}

interface ChampionStatJSON {
  patch: string;
  championId: number;
  teamPosition: string;
  wins: number;
  matches: number;
}

interface ChampionItemJSON {
  patch: string;
  championId: number;
  teamPosition: string;
  itemId: number;
  wins: number;
  matches: number;
}

interface ChampionItemSlotJSON {
  patch: string;
  championId: number;
  teamPosition: string;
  itemId: number;
  buildSlot: number;
  wins: number;
  matches: number;
}

interface ChampionMatchupJSON {
  patch: string;
  championId: number;
  teamPosition: string;
  enemyChampionId: number;
  wins: number;
  matches: number;
}

// Database singleton
let db: Database.Database | null = null;
let currentPatch: string = "";

function getDbPath(): string {
  const dataDir = path.join(process.cwd(), "data");
  if (!fs.existsSync(dataDir)) {
    fs.mkdirSync(dataDir, { recursive: true });
  }
  return path.join(dataDir, "stats.db");
}

function initSchema(database: Database.Database): void {
  database.exec(`
    CREATE TABLE IF NOT EXISTS data_version (
      id INTEGER PRIMARY KEY CHECK (id = 1),
      patch TEXT NOT NULL,
      updated_at TEXT NOT NULL
    );

    CREATE TABLE IF NOT EXISTS champion_stats (
      patch TEXT NOT NULL,
      champion_id INTEGER NOT NULL,
      team_position TEXT NOT NULL,
      wins INTEGER NOT NULL DEFAULT 0,
      matches INTEGER NOT NULL DEFAULT 0,
      PRIMARY KEY (patch, champion_id, team_position)
    );

    CREATE TABLE IF NOT EXISTS champion_items (
      patch TEXT NOT NULL,
      champion_id INTEGER NOT NULL,
      team_position TEXT NOT NULL,
      item_id INTEGER NOT NULL,
      wins INTEGER NOT NULL DEFAULT 0,
      matches INTEGER NOT NULL DEFAULT 0,
      PRIMARY KEY (patch, champion_id, team_position, item_id)
    );

    CREATE TABLE IF NOT EXISTS champion_item_slots (
      patch TEXT NOT NULL,
      champion_id INTEGER NOT NULL,
      team_position TEXT NOT NULL,
      item_id INTEGER NOT NULL,
      build_slot INTEGER NOT NULL,
      wins INTEGER NOT NULL DEFAULT 0,
      matches INTEGER NOT NULL DEFAULT 0,
      PRIMARY KEY (patch, champion_id, team_position, item_id, build_slot)
    );

    CREATE TABLE IF NOT EXISTS champion_matchups (
      patch TEXT NOT NULL,
      champion_id INTEGER NOT NULL,
      team_position TEXT NOT NULL,
      enemy_champion_id INTEGER NOT NULL,
      wins INTEGER NOT NULL DEFAULT 0,
      matches INTEGER NOT NULL DEFAULT 0,
      PRIMARY KEY (patch, champion_id, team_position, enemy_champion_id)
    );
  `);
}

function loadCurrentPatch(database: Database.Database): string {
  try {
    const row = database
      .prepare("SELECT patch FROM data_version WHERE id = 1")
      .get() as { patch: string } | undefined;
    return row?.patch || "";
  } catch {
    return "";
  }
}

export function getDb(): Database.Database {
  if (!db) {
    const dbPath = getDbPath();
    db = new Database(dbPath);
    initSchema(db);
    currentPatch = loadCurrentPatch(db);
    console.log(`[Stats] Database initialized, current patch: ${currentPatch || "none"}`);
  }
  return db;
}

export function getCurrentPatch(): string {
  if (!db) getDb();
  return currentPatch;
}

function clearLocalData(): void {
  const database = getDb();
  database.prepare("DELETE FROM data_version").run();
  currentPatch = "";
}

async function downloadAndImport(
  dataURL: string,
  expectedSha256: string,
  manifestVersion: string
): Promise<void> {
  console.log(`[Stats] Downloading data from: ${dataURL}`);

  const response = await fetch(dataURL);
  if (!response.ok) {
    throw new Error(`Data fetch returned status ${response.status}`);
  }

  const body = await response.text();

  // Verify SHA256 if provided
  if (expectedSha256) {
    const hash = crypto.createHash("sha256").update(body).digest("hex");
    if (hash !== expectedSha256) {
      throw new Error(`SHA256 mismatch: expected ${expectedSha256}, got ${hash}`);
    }
    console.log("[Stats] SHA256 verified successfully");
  }

  const data: DataExport = JSON.parse(body);
  importData(data, manifestVersion);
}

function importData(data: DataExport, version: string): void {
  const database = getDb();

  const transaction = database.transaction(() => {
    // Upsert champion_stats - add to existing values on conflict
    const insertStats = database.prepare(`
      INSERT INTO champion_stats (patch, champion_id, team_position, wins, matches)
      VALUES (?, ?, ?, ?, ?)
      ON CONFLICT(patch, champion_id, team_position) DO UPDATE SET
        wins = wins + excluded.wins,
        matches = matches + excluded.matches
    `);
    for (const stat of data.championStats || []) {
      insertStats.run(stat.patch, stat.championId, stat.teamPosition, stat.wins, stat.matches);
    }

    // Upsert champion_items - add to existing values on conflict
    const insertItems = database.prepare(`
      INSERT INTO champion_items (patch, champion_id, team_position, item_id, wins, matches)
      VALUES (?, ?, ?, ?, ?, ?)
      ON CONFLICT(patch, champion_id, team_position, item_id) DO UPDATE SET
        wins = wins + excluded.wins,
        matches = matches + excluded.matches
    `);
    for (const item of data.championItems || []) {
      insertItems.run(
        item.patch,
        item.championId,
        item.teamPosition,
        item.itemId,
        item.wins,
        item.matches
      );
    }

    // Upsert champion_item_slots - add to existing values on conflict
    const insertSlots = database.prepare(`
      INSERT INTO champion_item_slots (patch, champion_id, team_position, item_id, build_slot, wins, matches)
      VALUES (?, ?, ?, ?, ?, ?, ?)
      ON CONFLICT(patch, champion_id, team_position, item_id, build_slot) DO UPDATE SET
        wins = wins + excluded.wins,
        matches = matches + excluded.matches
    `);
    for (const slot of data.championItemSlots || []) {
      insertSlots.run(
        slot.patch,
        slot.championId,
        slot.teamPosition,
        slot.itemId,
        slot.buildSlot,
        slot.wins,
        slot.matches
      );
    }

    // Upsert champion_matchups - add to existing values on conflict
    const insertMatchups = database.prepare(`
      INSERT INTO champion_matchups (patch, champion_id, team_position, enemy_champion_id, wins, matches)
      VALUES (?, ?, ?, ?, ?, ?)
      ON CONFLICT(patch, champion_id, team_position, enemy_champion_id) DO UPDATE SET
        wins = wins + excluded.wins,
        matches = matches + excluded.matches
    `);
    for (const matchup of data.championMatchups || []) {
      insertMatchups.run(
        matchup.patch,
        matchup.championId,
        matchup.teamPosition,
        matchup.enemyChampionId,
        matchup.wins,
        matchup.matches
      );
    }

    // Update version
    database
      .prepare(
        `INSERT OR REPLACE INTO data_version (id, patch, updated_at)
         VALUES (1, ?, datetime('now'))`
      )
      .run(version);
  });

  transaction();

  console.log(
    `[Stats] Accumulated: ${data.championStats?.length || 0} champion stats, ` +
      `${data.championItems?.length || 0} item stats, ` +
      `${data.championItemSlots?.length || 0} item slot stats, ` +
      `${data.championMatchups?.length || 0} matchup stats`
  );

  currentPatch = version;
}

export async function checkForUpdates(
  manifestURL: string = DEFAULT_MANIFEST_URL
): Promise<void> {
  console.log(`[Stats] Checking for updates from: ${manifestURL}`);

  const response = await fetch(manifestURL);
  if (!response.ok) {
    throw new Error(`Manifest fetch returned status ${response.status}`);
  }

  const manifest: Manifest = await response.json();

  console.log(
    `[Stats] Remote version: ${manifest.version}, Local patch: ${currentPatch}, ForceReset: ${manifest.force_reset}`
  );

  // Force reset clears local data before comparing versions
  if (manifest.force_reset) {
    console.log("[Stats] Force reset requested - clearing local data");
    clearLocalData();
  }

  // Compare versions
  if (manifest.version && manifest.version <= currentPatch) {
    console.log("[Stats] Local data is up to date");
    return;
  }

  // Download and import new data
  await downloadAndImport(manifest.data_url, manifest.data_sha256, manifest.version);
  console.log(`[Stats] Updated to version: ${currentPatch}`);
}

export function hasData(): boolean {
  const database = getDb();
  try {
    const row = database.prepare("SELECT COUNT(*) as count FROM champion_stats").get() as {
      count: number;
    };
    return row.count > 0;
  } catch {
    return false;
  }
}

export async function forceUpdate(
  manifestURL: string = DEFAULT_MANIFEST_URL
): Promise<void> {
  console.log("[Stats] Force update requested - clearing local version");
  clearLocalData();
  await checkForUpdates(manifestURL);
}
