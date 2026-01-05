// Re-export Data Dragon functions for champion/item data
export {
  getDDragon,
  getDDragonVersion,
  getChampionName,
  getChampionIcon,
  getItemName,
  getItemIcon,
} from "./ddragon";

// Utility functions (synchronous, no data fetching)

export const roleDisplayNames: Record<string, string> = {
  top: "Top",
  jungle: "Jungle",
  middle: "Mid",
  bottom: "ADC",
  utility: "Support",
};

export const roleIcons: Record<string, string> = {
  top: "T",
  jungle: "J",
  middle: "M",
  bottom: "B",
  utility: "S",
};

export function getWinRateClass(winRate: number): string {
  if (winRate >= 52) return "wr-high";
  if (winRate >= 50) return "wr-mid";
  return "wr-low";
}

export function getTier(winRate: number, pickRate: number): string {
  if (winRate >= 53 && pickRate >= 3) return "S+";
  if (winRate >= 52 && pickRate >= 2) return "S";
  if (winRate >= 51 && pickRate >= 1) return "A";
  if (winRate >= 50) return "B";
  if (winRate >= 48) return "C";
  return "D";
}

export function getTierColor(tier: string): string {
  switch (tier) {
    case "S+":
      return "text-[#ff6b6b]";
    case "S":
      return "text-[var(--hextech-gold)]";
    case "A":
      return "text-[#4ade80]";
    case "B":
      return "text-[var(--arcane-cyan)]";
    case "C":
      return "text-[var(--text-secondary)]";
    default:
      return "text-[var(--text-muted)]";
  }
}
