// Data Dragon API - fetches latest champion and item data
// Cached at the Next.js level for performance

interface ChampionData {
  id: string; // e.g., "Ahri", "MonkeyKing"
  key: string; // e.g., "103"
  name: string; // e.g., "Ahri", "Wukong"
}

interface ItemData {
  name: string;
  gold: { total: number };
  into?: string[];
}

interface DDragonCache {
  version: string;
  champions: Map<number, { name: string; key: string }>;
  items: Map<number, string>;
  fetchedAt: number;
}

let cache: DDragonCache | null = null;
const CACHE_TTL = 1000 * 60 * 60; // 1 hour

async function fetchWithRetry(url: string, retries = 3): Promise<Response> {
  for (let i = 0; i < retries; i++) {
    try {
      const response = await fetch(url, {
        next: { revalidate: 3600 } // Cache for 1 hour
      });
      if (response.ok) return response;
    } catch (e) {
      if (i === retries - 1) throw e;
    }
  }
  throw new Error(`Failed to fetch ${url}`);
}

async function loadDDragonData(): Promise<DDragonCache> {
  // Return cached data if still valid
  if (cache && Date.now() - cache.fetchedAt < CACHE_TTL) {
    return cache;
  }

  console.log("[DDragon] Fetching latest version...");

  // Get latest version
  const versionsResp = await fetchWithRetry("https://ddragon.leagueoflegends.com/api/versions.json");
  const versions: string[] = await versionsResp.json();
  const version = versions[0];

  console.log(`[DDragon] Loading data for version ${version}...`);

  // Fetch champions and items in parallel
  const [champResp, itemResp] = await Promise.all([
    fetchWithRetry(`https://ddragon.leagueoflegends.com/cdn/${version}/data/en_US/champion.json`),
    fetchWithRetry(`https://ddragon.leagueoflegends.com/cdn/${version}/data/en_US/item.json`),
  ]);

  const champData: { data: Record<string, ChampionData> } = await champResp.json();
  const itemData: { data: Record<string, ItemData> } = await itemResp.json();

  // Build champion map (numeric key -> { name, iconKey })
  const champions = new Map<number, { name: string; key: string }>();
  for (const [iconKey, champ] of Object.entries(champData.data)) {
    const numericId = parseInt(champ.key);
    if (!isNaN(numericId)) {
      champions.set(numericId, {
        name: champ.name,
        key: iconKey, // This is the key used for icon URLs
      });
    }
  }

  // Build item map (item ID -> name)
  const items = new Map<number, string>();
  for (const [itemId, item] of Object.entries(itemData.data)) {
    const numericId = parseInt(itemId);
    if (!isNaN(numericId)) {
      items.set(numericId, item.name);
    }
  }

  console.log(`[DDragon] Loaded ${champions.size} champions, ${items.size} items`);

  cache = {
    version,
    champions,
    items,
    fetchedAt: Date.now(),
  };

  return cache;
}

// Export functions that pages can use
export async function getDDragonVersion(): Promise<string> {
  const data = await loadDDragonData();
  return data.version;
}

export async function getChampionData(): Promise<Map<number, { name: string; key: string }>> {
  const data = await loadDDragonData();
  return data.champions;
}

export async function getItemData(): Promise<Map<number, string>> {
  const data = await loadDDragonData();
  return data.items;
}

// Synchronous lookup functions (for use after data is loaded)
export async function getChampionName(id: number): Promise<string> {
  const champions = await getChampionData();
  return champions.get(id)?.name || `Champion ${id}`;
}

export async function getChampionIcon(id: number): Promise<string> {
  const data = await loadDDragonData();
  const champ = data.champions.get(id);
  if (!champ) return "";
  return `https://ddragon.leagueoflegends.com/cdn/${data.version}/img/champion/${champ.key}.png`;
}

export async function getItemName(id: number): Promise<string> {
  const items = await getItemData();
  return items.get(id) || `Item ${id}`;
}

export async function getItemIcon(id: number): Promise<string> {
  const version = await getDDragonVersion();
  return `https://ddragon.leagueoflegends.com/cdn/${version}/img/item/${id}.png`;
}

// Pre-load and return all data for pages that need multiple lookups
export async function getDDragon() {
  const data = await loadDDragonData();
  return {
    version: data.version,
    getChampionName: (id: number) => data.champions.get(id)?.name || `Champion ${id}`,
    getChampionIcon: (id: number) => {
      const champ = data.champions.get(id);
      if (!champ) return "";
      return `https://ddragon.leagueoflegends.com/cdn/${data.version}/img/champion/${champ.key}.png`;
    },
    getItemName: (id: number) => data.items.get(id) || `Item ${id}`,
    getItemIcon: (id: number) => `https://ddragon.leagueoflegends.com/cdn/${data.version}/img/item/${id}.png`,
  };
}
