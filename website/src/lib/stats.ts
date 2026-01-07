import { getDb, getCurrentPatch } from "./db";
import { unstable_cache } from "next/cache";

// Minimum games threshold for using current patch only
// If current patch has fewer games, fallback to aggregated data
const MIN_GAMES_FOR_CURRENT_PATCH = 1000;

export interface ItemOption {
  itemId: number;
  winRate: number;
  pickRate: number; // % of games this item was chosen in this slot (calculated from sampled data)
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

// Tier 2 boots item IDs
const BOOTS_IDS = [3006, 3009, 3020, 3047, 3111, 3117, 3158];

async function _fetchChampionData(
  championId: number,
  championName: string,
  role: string
): Promise<BuildData | null> {
  const client = getDb();
  const position = roleToPosition(role);

  const totalResult = await client.execute({
    sql: `SELECT COALESCE(SUM(matches), 0) as total FROM champion_stats
          WHERE champion_id = ? AND team_position = ?`,
    args: [championId, position],
  });

  const totalGames = (totalResult.rows[0]?.total as number) || 0;
  if (totalGames === 0) return null;

  const build = await constructBuildPathFromSlots(championId, position, totalGames);
  return { championId, championName, role, builds: [build] };
}

async function constructBuildPathFromSlots(
  championId: number,
  position: string,
  totalGames: number
): Promise<BuildPath> {
  const client = getDb();

  // Uses window function to calculate pick_rate from sampled data (avoids denominator trap)
  const getSlotItems = async (
    slot: number,
    limit: number,
    excludeItems: number[] = [],
    excludeBoots: boolean = false
  ): Promise<ItemOption[]> => {
    const excludeIds = [...excludeItems];
    if (excludeBoots) excludeIds.push(...BOOTS_IDS);

    let excludeClause = "";
    if (excludeIds.length > 0) {
      excludeClause = ` AND item_id NOT IN (${excludeIds.join(",")})`;
    }

    const result = await client.execute({
      sql: `SELECT
              item_id,
              SUM(wins) as wins,
              SUM(matches) as matches,
              CAST(SUM(matches) AS REAL) / SUM(SUM(matches)) OVER () * 100 as pick_rate
            FROM champion_item_slots
            WHERE champion_id = ? AND team_position = ? AND build_slot = ?${excludeClause}
            GROUP BY item_id
            ORDER BY SUM(matches) DESC
            LIMIT ?`,
      args: [championId, position, slot, limit],
    });

    return result.rows
      .filter((r) => (r.matches as number) > 0)
      .map((r) => ({
        itemId: r.item_id as number,
        winRate: ((r.wins as number) / (r.matches as number)) * 100,
        pickRate: r.pick_rate as number,
        games: r.matches as number,
      }));
  };

  const bootsResult = await client.execute({
    sql: `SELECT item_id, SUM(wins) as wins, SUM(matches) as matches
          FROM champion_item_slots
          WHERE champion_id = ? AND team_position = ? AND item_id IN (${BOOTS_IDS.join(",")})
          GROUP BY item_id
          ORDER BY SUM(matches) DESC
          LIMIT 1`,
    args: [championId, position],
  });

  const bestBoots = bootsResult.rows[0]?.item_id as number | undefined;
  const coreItems: number[] = [];
  let winRate = 0;

  for (let slot = 1; slot <= 3; slot++) {
    if (coreItems.length >= 2) break;
    const items = await getSlotItems(slot, 1, coreItems, true);
    if (items.length > 0) {
      coreItems.push(items[0].itemId);
      if (coreItems.length === 1) winRate = items[0].winRate;
    }
  }

  if (bestBoots) coreItems.push(bestBoots);

  const allExcluded = [...coreItems, ...BOOTS_IDS];
  const fourthItemOptions = (await getSlotItems(4, 5, allExcluded)).slice(0, 3);
  const fifthItemOptions = (await getSlotItems(5, 5, allExcluded)).slice(0, 3);
  const sixthItemOptions = (await getSlotItems(6, 5, allExcluded)).slice(0, 3);

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

export const fetchChampionData = unstable_cache(
  _fetchChampionData,
  ["champion-data"],
  { revalidate: 3600 }
);

async function _fetchMatchup(
  championId: number,
  enemyChampionId: number,
  role: string
): Promise<MatchupStat | null> {
  const client = getDb();
  const position = roleToPosition(role);

  const result = await client.execute({
    sql: `SELECT COALESCE(SUM(wins), 0) as wins, COALESCE(SUM(matches), 0) as matches
          FROM champion_matchups
          WHERE champion_id = ? AND team_position = ? AND enemy_champion_id = ?`,
    args: [championId, position, enemyChampionId],
  });

  const row = result.rows[0];
  if (!row || (row.matches as number) === 0) return null;

  return {
    enemyChampionId,
    wins: row.wins as number,
    matches: row.matches as number,
    winRate: ((row.wins as number) / (row.matches as number)) * 100,
  };
}

export const fetchMatchup = unstable_cache(_fetchMatchup, ["matchup"], { revalidate: 3600 });

async function _fetchAllMatchups(championId: number, role: string): Promise<MatchupStat[]> {
  const client = getDb();
  const position = roleToPosition(role);

  const result = await client.execute({
    sql: `SELECT enemy_champion_id, SUM(wins) as wins, SUM(matches) as matches
          FROM champion_matchups
          WHERE champion_id = ? AND team_position = ?
          GROUP BY enemy_champion_id
          ORDER BY SUM(matches) DESC`,
    args: [championId, position],
  });

  return result.rows
    .filter((r) => (r.matches as number) > 0)
    .map((r) => ({
      enemyChampionId: r.enemy_champion_id as number,
      wins: r.wins as number,
      matches: r.matches as number,
      winRate: ((r.wins as number) / (r.matches as number)) * 100,
    }));
}

export const fetchAllMatchups = unstable_cache(_fetchAllMatchups, ["all-matchups"], { revalidate: 3600 });

async function _fetchCounterMatchups(
  championId: number,
  role: string,
  limit: number = 5
): Promise<MatchupStat[]> {
  const client = getDb();
  const position = roleToPosition(role);

  const result = await client.execute({
    sql: `SELECT enemy_champion_id, SUM(wins) as wins, SUM(matches) as matches
          FROM champion_matchups
          WHERE champion_id = ? AND team_position = ?
          GROUP BY enemy_champion_id
          HAVING SUM(matches) >= 10
             AND (CAST(SUM(wins) AS REAL) / CAST(SUM(matches) AS REAL)) < 0.49
          ORDER BY (CAST(SUM(wins) AS REAL) / CAST(SUM(matches) AS REAL)) ASC
          LIMIT ?`,
    args: [championId, position, limit],
  });

  return result.rows
    .filter((r) => (r.matches as number) > 0)
    .map((r) => ({
      enemyChampionId: r.enemy_champion_id as number,
      wins: r.wins as number,
      matches: r.matches as number,
      winRate: ((r.wins as number) / (r.matches as number)) * 100,
    }));
}

export const fetchCounterMatchups = unstable_cache(_fetchCounterMatchups, ["counter-matchups"], { revalidate: 3600 });

async function _fetchAllChampionsByRole(role: string): Promise<ChampionWinRate[]> {
  const client = getDb();
  const position = roleToPosition(role);
  const currentPatch = await getCurrentPatch();

  // Check if current patch has enough games
  let currentPatchGames = 0;
  if (currentPatch) {
    const patchResult = await client.execute({
      sql: `SELECT COALESCE(SUM(matches), 0) as total FROM champion_stats WHERE team_position = ? AND patch = ?`,
      args: [position, currentPatch],
    });
    currentPatchGames = (patchResult.rows[0]?.total as number) || 0;
  }

  // Decide whether to use current patch only or aggregate
  const useCurrentPatchOnly = currentPatchGames >= MIN_GAMES_FOR_CURRENT_PATCH;

  let totalGames: number;
  let result;

  if (useCurrentPatchOnly) {
    // Current patch has enough data - use it exclusively
    const totalResult = await client.execute({
      sql: `SELECT COALESCE(SUM(matches), 0) as total FROM champion_stats WHERE team_position = ? AND patch = ?`,
      args: [position, currentPatch],
    });
    totalGames = (totalResult.rows[0]?.total as number) || 0;

    result = await client.execute({
      sql: `SELECT champion_id, SUM(wins) as wins, SUM(matches) as matches
            FROM champion_stats
            WHERE team_position = ? AND patch = ?
            GROUP BY champion_id
            HAVING SUM(matches) >= 100
            ORDER BY (CAST(SUM(wins) AS REAL) / CAST(SUM(matches) AS REAL)) DESC`,
      args: [position, currentPatch],
    });
  } else {
    // Not enough data in current patch - aggregate all patches
    const totalResult = await client.execute({
      sql: `SELECT COALESCE(SUM(matches), 0) as total FROM champion_stats WHERE team_position = ?`,
      args: [position],
    });
    totalGames = (totalResult.rows[0]?.total as number) || 0;

    result = await client.execute({
      sql: `SELECT champion_id, SUM(wins) as wins, SUM(matches) as matches
            FROM champion_stats
            WHERE team_position = ?
            GROUP BY champion_id
            HAVING SUM(matches) >= 100
            ORDER BY (CAST(SUM(wins) AS REAL) / CAST(SUM(matches) AS REAL)) DESC`,
      args: [position],
    });
  }

  return result.rows
    .filter((r) => (r.matches as number) > 0)
    .map((r) => ({
      championId: r.champion_id as number,
      wins: r.wins as number,
      matches: r.matches as number,
      winRate: ((r.wins as number) / (r.matches as number)) * 100,
      pickRate: totalGames > 0 ? ((r.matches as number) / totalGames) * 100 : 0,
    }));
}

export const fetchAllChampionsByRole = unstable_cache(_fetchAllChampionsByRole, ["champions-by-role"], { revalidate: 3600 });

async function _fetchBestMatchups(
  championId: number,
  role: string,
  limit: number = 5
): Promise<MatchupStat[]> {
  const client = getDb();
  const position = roleToPosition(role);

  const result = await client.execute({
    sql: `SELECT enemy_champion_id, SUM(wins) as wins, SUM(matches) as matches
          FROM champion_matchups
          WHERE champion_id = ? AND team_position = ?
          GROUP BY enemy_champion_id
          HAVING SUM(matches) >= 10
             AND (CAST(SUM(wins) AS REAL) / CAST(SUM(matches) AS REAL)) > 0.51
          ORDER BY (CAST(SUM(wins) AS REAL) / CAST(SUM(matches) AS REAL)) DESC
          LIMIT ?`,
    args: [championId, position, limit],
  });

  return result.rows
    .filter((r) => (r.matches as number) > 0)
    .map((r) => ({
      enemyChampionId: r.enemy_champion_id as number,
      wins: r.wins as number,
      matches: r.matches as number,
      winRate: ((r.wins as number) / (r.matches as number)) * 100,
    }));
}

export const fetchBestMatchups = unstable_cache(_fetchBestMatchups, ["best-matchups"], { revalidate: 3600 });

async function _fetchChampionStats(championId: number, role: string): Promise<ChampionWinRate | null> {
  const client = getDb();
  const position = roleToPosition(role);

  const totalResult = await client.execute({
    sql: `SELECT COALESCE(SUM(matches), 0) as total FROM champion_stats WHERE team_position = ?`,
    args: [position],
  });

  const totalGames = (totalResult.rows[0]?.total as number) || 0;

  const result = await client.execute({
    sql: `SELECT SUM(wins) as wins, SUM(matches) as matches
          FROM champion_stats
          WHERE champion_id = ? AND team_position = ?`,
    args: [championId, position],
  });

  const row = result.rows[0];
  if (!row || (row.matches as number) === 0) return null;

  return {
    championId,
    wins: row.wins as number,
    matches: row.matches as number,
    winRate: ((row.wins as number) / (row.matches as number)) * 100,
    pickRate: totalGames > 0 ? ((row.matches as number) / totalGames) * 100 : 0,
  };
}

export const fetchChampionStats = unstable_cache(_fetchChampionStats, ["champion-stats"], { revalidate: 3600 });

async function _fetchChampionRoles(championId: number): Promise<string[]> {
  const client = getDb();

  const result = await client.execute({
    sql: `SELECT team_position, SUM(matches) as matches
          FROM champion_stats
          WHERE champion_id = ?
          GROUP BY team_position
          HAVING SUM(matches) >= 50
          ORDER BY SUM(matches) DESC`,
    args: [championId],
  });

  return result.rows.map((r) => (r.team_position as string).toLowerCase());
}

export const fetchChampionRoles = unstable_cache(_fetchChampionRoles, ["champion-roles"], { revalidate: 3600 });

async function _fetchTopChampionsByRole(role: string, limit: number = 5): Promise<ChampionWinRate[]> {
  const client = getDb();
  const position = roleToPosition(role);
  const currentPatch = await getCurrentPatch();

  // Check if current patch has enough games
  let currentPatchGames = 0;
  if (currentPatch) {
    const patchResult = await client.execute({
      sql: `SELECT COALESCE(SUM(matches), 0) as total FROM champion_stats WHERE team_position = ? AND patch = ?`,
      args: [position, currentPatch],
    });
    currentPatchGames = (patchResult.rows[0]?.total as number) || 0;
  }

  // Decide whether to use current patch only or aggregate
  const useCurrentPatchOnly = currentPatchGames >= MIN_GAMES_FOR_CURRENT_PATCH;

  let totalGames: number;
  let result;

  if (useCurrentPatchOnly) {
    // Current patch has enough data - use it exclusively
    console.log(`[Stats] Using current patch ${currentPatch} only for ${role} (${currentPatchGames} games)`);

    const totalResult = await client.execute({
      sql: `SELECT COALESCE(SUM(matches), 0) as total FROM champion_stats WHERE team_position = ? AND patch = ?`,
      args: [position, currentPatch],
    });
    totalGames = (totalResult.rows[0]?.total as number) || 0;

    result = await client.execute({
      sql: `SELECT champion_id, SUM(wins) as wins, SUM(matches) as matches
            FROM champion_stats
            WHERE team_position = ? AND patch = ?
            GROUP BY champion_id
            HAVING SUM(matches) >= 100
            ORDER BY (CAST(SUM(wins) AS REAL) / CAST(SUM(matches) AS REAL)) DESC
            LIMIT ?`,
      args: [position, currentPatch, limit],
    });
  } else {
    // Not enough data in current patch - aggregate all patches
    console.log(`[Stats] Aggregating all patches for ${role} (current patch ${currentPatch} has only ${currentPatchGames} games)`);

    const totalResult = await client.execute({
      sql: `SELECT COALESCE(SUM(matches), 0) as total FROM champion_stats WHERE team_position = ?`,
      args: [position],
    });
    totalGames = (totalResult.rows[0]?.total as number) || 0;

    result = await client.execute({
      sql: `SELECT champion_id, SUM(wins) as wins, SUM(matches) as matches
            FROM champion_stats
            WHERE team_position = ?
            GROUP BY champion_id
            HAVING SUM(matches) >= 100
            ORDER BY (CAST(SUM(wins) AS REAL) / CAST(SUM(matches) AS REAL)) DESC
            LIMIT ?`,
      args: [position, limit],
    });
  }

  return result.rows
    .filter((r) => (r.matches as number) > 0)
    .map((r) => ({
      championId: r.champion_id as number,
      wins: r.wins as number,
      matches: r.matches as number,
      winRate: ((r.wins as number) / (r.matches as number)) * 100,
      pickRate: totalGames > 0 ? ((r.matches as number) / totalGames) * 100 : 0,
    }));
}

export const fetchTopChampionsByRole = unstable_cache(_fetchTopChampionsByRole, ["top-champions"], { revalidate: 3600 });

export async function fetchAllRolesTopChampions(limit: number = 5): Promise<Record<string, ChampionWinRate[]>> {
  const roles = ["top", "jungle", "middle", "bottom", "utility"];
  const result: Record<string, ChampionWinRate[]> = {};
  for (const role of roles) {
    result[role] = await fetchTopChampionsByRole(role, limit);
  }
  return result;
}

async function _getStatsInfo(): Promise<{
  patch: string;
  hasData: boolean;
  championCount: number;
  matchupCount: number;
}> {
  const client = getDb();
  const patch = await getCurrentPatch();

  try {
    const champResult = await client.execute(
      "SELECT COUNT(DISTINCT champion_id) as count FROM champion_stats"
    );
    const matchupResult = await client.execute(
      "SELECT COUNT(*) as count FROM champion_matchups"
    );

    return {
      patch,
      hasData: (champResult.rows[0]?.count as number) > 0,
      championCount: champResult.rows[0]?.count as number,
      matchupCount: matchupResult.rows[0]?.count as number,
    };
  } catch {
    return { patch: "", hasData: false, championCount: 0, matchupCount: 0 };
  }
}

export const getStatsInfo = unstable_cache(_getStatsInfo, ["stats-info"], { revalidate: 3600 });
