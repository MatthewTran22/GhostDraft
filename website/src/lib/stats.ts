import { getDb, getCurrentPatch } from "./db";

export interface ItemOption {
  itemId: number;
  winRate: number;
  games: number;
}

export interface BuildPath {
  name: string;
  winRate: number;
  games: number;
  coreItems: number[];
  fourthItemOptions: ItemOption[];
  fifthItemOptions: ItemOption[];
  sixthItemOptions: ItemOption[];
}

export interface BuildData {
  championId: number;
  championName: string;
  role: string;
  builds: BuildPath[];
}

export interface MatchupStat {
  enemyChampionId: number;
  wins: number;
  matches: number;
  winRate: number;
}

export interface ChampionWinRate {
  championId: number;
  wins: number;
  matches: number;
  winRate: number;
  pickRate: number;
}

function roleToPosition(role: string): string {
  switch (role.toLowerCase()) {
    case "top":
      return "TOP";
    case "jungle":
      return "JUNGLE";
    case "middle":
    case "mid":
      return "MIDDLE";
    case "bottom":
    case "adc":
      return "BOTTOM";
    case "utility":
    case "support":
      return "UTILITY";
    default:
      return "MIDDLE";
  }
}

export function fetchChampionData(
  championId: number,
  championName: string,
  role: string
): BuildData | null {
  const db = getDb();
  const position = roleToPosition(role);

  // Get total games for this champion/position
  const totalRow = db
    .prepare(
      `SELECT COALESCE(SUM(matches), 0) as total FROM champion_stats
       WHERE champion_id = ? AND team_position = ?`
    )
    .get(championId, position) as { total: number };

  if (!totalRow || totalRow.total === 0) {
    return null;
  }

  const totalGames = totalRow.total;

  // Build from slots
  const build = constructBuildPathFromSlots(championId, position, totalGames);

  return {
    championId,
    championName,
    role,
    builds: [build],
  };
}

// Tier 2 boots item IDs
const BOOTS_IDS = [3006, 3009, 3020, 3047, 3111, 3117, 3158];

function constructBuildPathFromSlots(
  championId: number,
  position: string,
  totalGames: number
): BuildPath {
  const db = getDb();

  const getSlotItems = (
    slot: number,
    limit: number,
    excludeItems: number[] = [],
    excludeBoots: boolean = false
  ): ItemOption[] => {
    // Build exclusion clause
    const excludeIds = [...excludeItems];
    if (excludeBoots) {
      excludeIds.push(...BOOTS_IDS);
    }

    let excludeClause = "";
    if (excludeIds.length > 0) {
      excludeClause = ` AND item_id NOT IN (${excludeIds.join(",")})`;
    }

    const rows = db
      .prepare(
        `SELECT item_id, SUM(wins) as wins, SUM(matches) as matches
         FROM champion_item_slots
         WHERE champion_id = ? AND team_position = ? AND build_slot = ?${excludeClause}
         GROUP BY item_id
         ORDER BY SUM(matches) DESC
         LIMIT ?`
      )
      .all(championId, position, slot, limit) as Array<{
      item_id: number;
      wins: number;
      matches: number;
    }>;

    return rows
      .filter((r) => r.matches > 0)
      .map((r) => ({
        itemId: r.item_id,
        winRate: (r.wins / r.matches) * 100,
        games: r.matches,
      }));
  };

  // Get best boots across all slots
  const bootsQuery = db
    .prepare(
      `SELECT item_id, SUM(wins) as wins, SUM(matches) as matches
       FROM champion_item_slots
       WHERE champion_id = ? AND team_position = ? AND item_id IN (${BOOTS_IDS.join(",")})
       GROUP BY item_id
       ORDER BY SUM(matches) DESC
       LIMIT 1`
    )
    .get(championId, position) as { item_id: number; wins: number; matches: number } | undefined;

  const bestBoots = bootsQuery?.item_id;

  // Get 2 core items (excluding boots and duplicates)
  const coreItems: number[] = [];
  let winRate = 0;

  for (let slot = 1; slot <= 3; slot++) {
    if (coreItems.length >= 2) break;
    const items = getSlotItems(slot, 1, coreItems, true); // exclude boots
    if (items.length > 0) {
      coreItems.push(items[0].itemId);
      if (coreItems.length === 1) {
        winRate = items[0].winRate;
      }
    }
  }

  // Add boots to core items if found
  if (bestBoots) {
    coreItems.push(bestBoots);
  }

  // Get 4th, 5th, 6th item options, excluding core items and boots
  const allExcluded = [...coreItems, ...BOOTS_IDS];
  const fourthItemOptions = getSlotItems(4, 5, allExcluded).slice(0, 3);
  const fifthItemOptions = getSlotItems(5, 5, allExcluded).slice(0, 3);
  const sixthItemOptions = getSlotItems(6, 5, allExcluded).slice(0, 3);

  return {
    name: "Recommended Build",
    winRate,
    games: totalGames,
    coreItems,
    fourthItemOptions,
    fifthItemOptions,
    sixthItemOptions,
  };
}

export function fetchMatchup(
  championId: number,
  enemyChampionId: number,
  role: string
): MatchupStat | null {
  const db = getDb();
  const position = roleToPosition(role);

  const row = db
    .prepare(
      `SELECT COALESCE(SUM(wins), 0) as wins, COALESCE(SUM(matches), 0) as matches
       FROM champion_matchups
       WHERE champion_id = ? AND team_position = ? AND enemy_champion_id = ?`
    )
    .get(championId, position, enemyChampionId) as { wins: number; matches: number };

  if (!row || row.matches === 0) {
    return null;
  }

  return {
    enemyChampionId,
    wins: row.wins,
    matches: row.matches,
    winRate: (row.wins / row.matches) * 100,
  };
}

export function fetchAllMatchups(championId: number, role: string): MatchupStat[] {
  const db = getDb();
  const position = roleToPosition(role);

  const rows = db
    .prepare(
      `SELECT enemy_champion_id, SUM(wins) as wins, SUM(matches) as matches
       FROM champion_matchups
       WHERE champion_id = ? AND team_position = ?
       GROUP BY enemy_champion_id
       ORDER BY SUM(matches) DESC`
    )
    .all(championId, position) as Array<{
    enemy_champion_id: number;
    wins: number;
    matches: number;
  }>;

  return rows
    .filter((r) => r.matches > 0)
    .map((r) => ({
      enemyChampionId: r.enemy_champion_id,
      wins: r.wins,
      matches: r.matches,
      winRate: (r.wins / r.matches) * 100,
    }));
}

export function fetchCounterMatchups(
  championId: number,
  role: string,
  limit: number = 5
): MatchupStat[] {
  const db = getDb();
  const position = roleToPosition(role);

  // Counters = matchups where our win rate is < 49%
  const rows = db
    .prepare(
      `SELECT enemy_champion_id, SUM(wins) as wins, SUM(matches) as matches
       FROM champion_matchups
       WHERE champion_id = ? AND team_position = ?
       GROUP BY enemy_champion_id
       HAVING SUM(matches) >= 10
          AND (CAST(SUM(wins) AS REAL) / CAST(SUM(matches) AS REAL)) < 0.49
       ORDER BY (CAST(SUM(wins) AS REAL) / CAST(SUM(matches) AS REAL)) ASC
       LIMIT ?`
    )
    .all(championId, position, limit) as Array<{
    enemy_champion_id: number;
    wins: number;
    matches: number;
  }>;

  return rows
    .filter((r) => r.matches > 0)
    .map((r) => ({
      enemyChampionId: r.enemy_champion_id,
      wins: r.wins,
      matches: r.matches,
      winRate: (r.wins / r.matches) * 100,
    }));
}

export function fetchAllChampionsByRole(role: string): ChampionWinRate[] {
  const db = getDb();
  const position = roleToPosition(role);

  // Get total games for pick rate calculation
  const totalRow = db
    .prepare(
      `SELECT COALESCE(SUM(matches), 0) as total FROM champion_stats
       WHERE team_position = ?`
    )
    .get(position) as { total: number };

  const totalGames = totalRow?.total || 0;

  const rows = db
    .prepare(
      `SELECT champion_id, SUM(wins) as wins, SUM(matches) as matches
       FROM champion_stats
       WHERE team_position = ?
       GROUP BY champion_id
       HAVING SUM(matches) >= 50
       ORDER BY (CAST(SUM(wins) AS REAL) / CAST(SUM(matches) AS REAL)) DESC`
    )
    .all(position) as Array<{
    champion_id: number;
    wins: number;
    matches: number;
  }>;

  return rows
    .filter((r) => r.matches > 0)
    .map((r) => ({
      championId: r.champion_id,
      wins: r.wins,
      matches: r.matches,
      winRate: (r.wins / r.matches) * 100,
      pickRate: totalGames > 0 ? (r.matches / totalGames) * 100 : 0,
    }));
}

export function fetchBestMatchups(
  championId: number,
  role: string,
  limit: number = 5
): MatchupStat[] {
  const db = getDb();
  const position = roleToPosition(role);

  // Best matchups = matchups where our win rate is > 51%
  const rows = db
    .prepare(
      `SELECT enemy_champion_id, SUM(wins) as wins, SUM(matches) as matches
       FROM champion_matchups
       WHERE champion_id = ? AND team_position = ?
       GROUP BY enemy_champion_id
       HAVING SUM(matches) >= 10
          AND (CAST(SUM(wins) AS REAL) / CAST(SUM(matches) AS REAL)) > 0.51
       ORDER BY (CAST(SUM(wins) AS REAL) / CAST(SUM(matches) AS REAL)) DESC
       LIMIT ?`
    )
    .all(championId, position, limit) as Array<{
    enemy_champion_id: number;
    wins: number;
    matches: number;
  }>;

  return rows
    .filter((r) => r.matches > 0)
    .map((r) => ({
      enemyChampionId: r.enemy_champion_id,
      wins: r.wins,
      matches: r.matches,
      winRate: (r.wins / r.matches) * 100,
    }));
}

export function fetchChampionStats(
  championId: number,
  role: string
): ChampionWinRate | null {
  const db = getDb();
  const position = roleToPosition(role);

  // Get total games for pick rate calculation
  const totalRow = db
    .prepare(
      `SELECT COALESCE(SUM(matches), 0) as total FROM champion_stats
       WHERE team_position = ?`
    )
    .get(position) as { total: number };

  const totalGames = totalRow?.total || 0;

  const row = db
    .prepare(
      `SELECT SUM(wins) as wins, SUM(matches) as matches
       FROM champion_stats
       WHERE champion_id = ? AND team_position = ?`
    )
    .get(championId, position) as { wins: number; matches: number } | undefined;

  if (!row || row.matches === 0) {
    return null;
  }

  return {
    championId,
    wins: row.wins,
    matches: row.matches,
    winRate: (row.wins / row.matches) * 100,
    pickRate: totalGames > 0 ? (row.matches / totalGames) * 100 : 0,
  };
}

export function fetchChampionRoles(championId: number): string[] {
  const db = getDb();

  const rows = db
    .prepare(
      `SELECT team_position, SUM(matches) as matches
       FROM champion_stats
       WHERE champion_id = ?
       GROUP BY team_position
       HAVING SUM(matches) >= 50
       ORDER BY SUM(matches) DESC`
    )
    .all(championId) as Array<{ team_position: string; matches: number }>;

  return rows.map((r) => r.team_position.toLowerCase());
}

export function fetchTopChampionsByRole(
  role: string,
  limit: number = 5
): ChampionWinRate[] {
  const db = getDb();
  const position = roleToPosition(role);

  // Get total games for pick rate calculation
  const totalRow = db
    .prepare(
      `SELECT COALESCE(SUM(matches), 0) as total FROM champion_stats
       WHERE team_position = ?`
    )
    .get(position) as { total: number };

  const totalGames = totalRow?.total || 0;

  const rows = db
    .prepare(
      `SELECT champion_id, SUM(wins) as wins, SUM(matches) as matches
       FROM champion_stats
       WHERE team_position = ?
       GROUP BY champion_id
       HAVING SUM(matches) >= 100
       ORDER BY (CAST(SUM(wins) AS REAL) / CAST(SUM(matches) AS REAL)) DESC
       LIMIT ?`
    )
    .all(position, limit) as Array<{
    champion_id: number;
    wins: number;
    matches: number;
  }>;

  return rows
    .filter((r) => r.matches > 0)
    .map((r) => ({
      championId: r.champion_id,
      wins: r.wins,
      matches: r.matches,
      winRate: (r.wins / r.matches) * 100,
      pickRate: totalGames > 0 ? (r.matches / totalGames) * 100 : 0,
    }));
}

export function fetchAllRolesTopChampions(
  limit: number = 5
): Record<string, ChampionWinRate[]> {
  const roles = ["top", "jungle", "middle", "bottom", "utility"];
  const result: Record<string, ChampionWinRate[]> = {};

  for (const role of roles) {
    result[role] = fetchTopChampionsByRole(role, limit);
  }

  return result;
}

export function getStatsInfo(): {
  patch: string;
  hasData: boolean;
  championCount: number;
  matchupCount: number;
} {
  const db = getDb();
  const patch = getCurrentPatch();

  try {
    const champRow = db
      .prepare("SELECT COUNT(DISTINCT champion_id) as count FROM champion_stats")
      .get() as { count: number };

    const matchupRow = db.prepare("SELECT COUNT(*) as count FROM champion_matchups").get() as {
      count: number;
    };

    return {
      patch,
      hasData: champRow.count > 0,
      championCount: champRow.count,
      matchupCount: matchupRow.count,
    };
  } catch {
    return {
      patch: "",
      hasData: false,
      championCount: 0,
      matchupCount: 0,
    };
  }
}
