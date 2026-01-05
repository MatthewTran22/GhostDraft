import { fetchAllChampionsByRole, getStatsInfo } from "@/lib/stats";
import { checkForUpdates, hasData } from "@/lib/db";
import Link from "next/link";
import type { Metadata } from "next";
import {
  getDDragon,
  roleDisplayNames,
  getWinRateClass,
  getTier,
  getTierColor,
} from "@/lib/champions";

export const metadata: Metadata = {
  title: "Champion Tier List - GhostDraft",
  description:
    "View League of Legends champion win rates, tier lists, and meta picks by role.",
};

// Revalidate every hour
export const revalidate = 3600;

interface PageProps {
  searchParams: Promise<{ role?: string }>;
}

export default async function StatsPage({ searchParams }: PageProps) {
  const params = await searchParams;
  const selectedRole = params.role || "top";

  // Load Data Dragon data
  const ddragon = await getDDragon();

  // Check for updates and ensure we have data
  try {
    if (!hasData()) {
      await checkForUpdates();
    }
  } catch (error) {
    console.error("Failed to check for updates:", error);
  }

  const statsInfo = getStatsInfo();
  const champions = fetchAllChampionsByRole(selectedRole);

  const roles = ["top", "jungle", "middle", "bottom", "utility"];

  return (
    <div className="max-w-6xl mx-auto px-6 py-16">
      <div className="text-center mb-12">
        <h1 className="text-4xl md:text-5xl font-display font-bold text-[var(--pale-gold)] mb-4 text-glow">
          Champion Tier List
        </h1>
        <p className="text-[var(--text-secondary)] max-w-2xl mx-auto">
          Full tier rankings based on win rate and pick rate. Click any champion for detailed stats.
        </p>
        {statsInfo.patch && (
          <p className="text-[var(--arcane-cyan)] text-sm mt-4 text-glow-cyan">
            Patch {statsInfo.patch} | {statsInfo.championCount} champions |{" "}
            {statsInfo.matchupCount.toLocaleString()} matchups
          </p>
        )}
      </div>

      {/* Role Tabs */}
      <div className="flex justify-center gap-2 mb-8 flex-wrap">
        {roles.map((role) => (
          <Link
            key={role}
            href={`/stats?role=${role}`}
            className={`px-6 py-3 rounded-lg font-medium transition-all ${
              selectedRole === role
                ? "bg-[var(--hextech-gold)]/20 text-[var(--hextech-gold)] border border-[var(--hextech-gold)]/50"
                : "bg-[var(--arcane-blue)]/30 text-[var(--text-secondary)] border border-[var(--arcane-blue)] hover:border-[var(--hextech-gold)]/30 hover:text-[var(--text-primary)]"
            }`}
          >
            {roleDisplayNames[role]}
          </Link>
        ))}
      </div>

      {!statsInfo.hasData ? (
        <div className="text-center py-16">
          <div className="hex-card rounded-xl p-8 max-w-md mx-auto">
            <p className="text-[var(--text-secondary)] mb-4">No stats data available yet.</p>
            <p className="text-[var(--text-muted)] text-sm">
              Stats data is downloaded automatically. Please refresh the page.
            </p>
          </div>
        </div>
      ) : (
        <div className="hex-card rounded-xl overflow-hidden">
          {/* Table Header */}
          <div className="bg-gradient-to-r from-[var(--deep-navy)] to-[var(--arcane-blue)] px-6 py-4 border-b border-[var(--hextech-gold)]/20">
            <div className="grid grid-cols-12 gap-4 text-sm font-medium text-[var(--text-secondary)]">
              <div className="col-span-1 text-center">#</div>
              <div className="col-span-1">Tier</div>
              <div className="col-span-4">Champion</div>
              <div className="col-span-2 text-center">Win Rate</div>
              <div className="col-span-2 text-center">Pick Rate</div>
              <div className="col-span-2 text-center">Games</div>
            </div>
          </div>

          {/* Champion Rows */}
          <div className="divide-y divide-[var(--hextech-gold)]/10">
            {champions.length === 0 ? (
              <div className="px-6 py-12 text-center text-[var(--text-muted)]">
                No data available for this role
              </div>
            ) : (
              champions.map((champ, index) => {
                const tier = getTier(champ.winRate, champ.pickRate);
                const tierColor = getTierColor(tier);

                return (
                  <Link
                    key={champ.championId}
                    href={`/stats/${champ.championId}?role=${selectedRole}`}
                    className="grid grid-cols-12 gap-4 px-6 py-4 items-center hover:bg-[var(--arcane-blue)]/20 transition-all group"
                  >
                    <div className="col-span-1 text-center">
                      <span
                        className={`font-bold ${
                          index < 3
                            ? "text-[var(--hextech-gold)]"
                            : "text-[var(--text-muted)]"
                        }`}
                      >
                        {index + 1}
                      </span>
                    </div>
                    <div className="col-span-1">
                      <span className={`font-bold font-display text-lg ${tierColor}`}>
                        {tier}
                      </span>
                    </div>
                    <div className="col-span-4 flex items-center gap-3">
                      <div className="w-10 h-10 rounded-full overflow-hidden border-2 border-[var(--hextech-gold)]/30 group-hover:border-[var(--hextech-gold)]/60 transition-all">
                        <img
                          src={ddragon.getChampionIcon(champ.championId)}
                          alt={ddragon.getChampionName(champ.championId)}
                          className="w-full h-full object-cover"
                          loading="lazy"
                        />
                      </div>
                      <span className="text-[var(--text-primary)] font-medium group-hover:text-[var(--pale-gold)] transition-colors">
                        {ddragon.getChampionName(champ.championId)}
                      </span>
                    </div>
                    <div className="col-span-2 text-center">
                      <span className={`font-semibold ${getWinRateClass(champ.winRate)}`}>
                        {champ.winRate.toFixed(2)}%
                      </span>
                    </div>
                    <div className="col-span-2 text-center">
                      <span className="text-[var(--text-secondary)]">
                        {champ.pickRate.toFixed(2)}%
                      </span>
                    </div>
                    <div className="col-span-2 text-center">
                      <span className="text-[var(--text-muted)]">
                        {champ.matches.toLocaleString()}
                      </span>
                    </div>
                  </Link>
                );
              })
            )}
          </div>
        </div>
      )}

      <div className="mt-12 text-center">
        <Link href="/#download" className="btn-hextech text-lg">
          Download GhostDraft for Live Stats
        </Link>
        <p className="text-[var(--text-muted)] text-sm mt-4">
          Get real-time matchup data and build recommendations during champion select.
        </p>
      </div>
    </div>
  );
}
