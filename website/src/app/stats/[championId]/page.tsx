import {
  fetchChampionData,
  fetchChampionStats,
  fetchChampionRoles,
  fetchBestMatchups,
  fetchCounterMatchups,
  getStatsInfo,
} from "@/lib/stats";
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

interface PageProps {
  params: Promise<{ championId: string }>;
  searchParams: Promise<{ role?: string }>;
}

export async function generateMetadata({ params }: PageProps): Promise<Metadata> {
  const { championId } = await params;
  const champId = parseInt(championId);
  const ddragon = await getDDragon();
  const name = ddragon.getChampionName(champId);

  return {
    title: `${name} Stats & Build - GhostDraft`,
    description: `${name} win rates, best builds, counters, and matchups for League of Legends.`,
  };
}

export const revalidate = 3600;

export default async function ChampionPage({ params, searchParams }: PageProps) {
  const { championId } = await params;
  const { role: queryRole } = await searchParams;

  // Load Data Dragon data
  const ddragon = await getDDragon();

  const champId = parseInt(championId);
  const champName = ddragon.getChampionName(champId);

  // Check for updates
  try {
    if (!hasData()) {
      await checkForUpdates();
    }
  } catch (error) {
    console.error("Failed to check for updates:", error);
  }

  const statsInfo = getStatsInfo();
  const availableRoles = fetchChampionRoles(champId);
  const selectedRole = queryRole && availableRoles.includes(queryRole) ? queryRole : availableRoles[0] || "middle";

  const champStats = fetchChampionStats(champId, selectedRole);
  const buildData = fetchChampionData(champId, champName, selectedRole);
  const counters = fetchCounterMatchups(champId, selectedRole, 5);
  const bestMatchups = fetchBestMatchups(champId, selectedRole, 5);

  if (!champStats) {
    return (
      <div className="max-w-6xl mx-auto px-6 py-16">
        <div className="text-center py-16">
          <div className="hex-card rounded-xl p-8 max-w-md mx-auto">
            <p className="text-[var(--text-secondary)] mb-4">No data available for {champName}.</p>
            <Link href="/stats" className="text-[var(--hextech-gold)] hover:text-[var(--bright-gold)] hover-line">
              &larr; Back to Tier List
            </Link>
          </div>
        </div>
      </div>
    );
  }

  const tier = getTier(champStats.winRate, champStats.pickRate);
  const tierColor = getTierColor(tier);

  return (
    <div className="max-w-6xl mx-auto px-6 py-16">
      {/* Back Link */}
      <Link
        href={`/stats?role=${selectedRole}`}
        className="text-[var(--text-secondary)] hover:text-[var(--hextech-gold)] transition-colors hover-line mb-8 inline-block"
      >
        &larr; Back to Tier List
      </Link>

      {/* Champion Header */}
      <div className="hex-card rounded-xl p-8 mb-8">
        <div className="flex flex-col md:flex-row items-start md:items-center gap-6">
          <div className="relative">
            <div className="w-24 h-24 rounded-full overflow-hidden border-4 border-[var(--hextech-gold)]/50 pulse-glow">
              <img
                src={ddragon.getChampionIcon(champId)}
                alt={champName}
                className="w-full h-full object-cover"
              />
            </div>
            <div
              className={`absolute -bottom-2 -right-2 w-10 h-10 rounded-full flex items-center justify-center font-display font-bold text-lg border-2 border-[var(--void-black)] ${tierColor} bg-[var(--deep-navy)]`}
            >
              {tier}
            </div>
          </div>

          <div className="flex-1">
            <h1 className="text-4xl font-display font-bold text-[var(--pale-gold)] mb-2 text-glow">
              {champName}
            </h1>
            <div className="flex flex-wrap gap-4 text-lg">
              <div>
                <span className="text-[var(--text-muted)]">Win Rate: </span>
                <span className={`font-semibold ${getWinRateClass(champStats.winRate)}`}>
                  {champStats.winRate.toFixed(2)}%
                </span>
              </div>
              <div>
                <span className="text-[var(--text-muted)]">Pick Rate: </span>
                <span className="text-[var(--text-secondary)] font-semibold">
                  {champStats.pickRate.toFixed(2)}%
                </span>
              </div>
              <div>
                <span className="text-[var(--text-muted)]">Games: </span>
                <span className="text-[var(--text-secondary)] font-semibold">
                  {champStats.matches.toLocaleString()}
                </span>
              </div>
            </div>
          </div>

          {/* Role Tabs */}
          {availableRoles.length > 1 && (
            <div className="flex gap-2 flex-wrap">
              {availableRoles.map((role) => (
                <Link
                  key={role}
                  href={`/stats/${champId}?role=${role}`}
                  className={`px-4 py-2 rounded-lg text-sm font-medium transition-all ${
                    selectedRole === role
                      ? "bg-[var(--hextech-gold)]/20 text-[var(--hextech-gold)] border border-[var(--hextech-gold)]/50"
                      : "bg-[var(--arcane-blue)]/30 text-[var(--text-secondary)] border border-[var(--arcane-blue)] hover:border-[var(--hextech-gold)]/30"
                  }`}
                >
                  {roleDisplayNames[role] || role}
                </Link>
              ))}
            </div>
          )}
        </div>
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-2 gap-8">
        {/* Build Section */}
        {buildData && buildData.builds.length > 0 && (
          <div className="hex-card rounded-xl overflow-hidden">
            <div className="bg-gradient-to-r from-[var(--deep-navy)] to-[var(--arcane-blue)] px-6 py-4 border-b border-[var(--hextech-gold)]/20">
              <h2 className="text-xl font-display font-semibold text-[var(--pale-gold)]">
                Recommended Build
              </h2>
            </div>
            <div className="p-6 space-y-6">
              {/* Core Items */}
              <div>
                <h3 className="text-sm font-medium text-[var(--text-secondary)] mb-3">Core Items</h3>
                <div className="flex gap-3 flex-wrap">
                  {buildData.builds[0].coreItems.map((itemId, index) => (
                    <div key={index} className="text-center">
                      <div className="w-14 h-14 rounded-lg overflow-hidden border-2 border-[var(--hextech-gold)]/30 bg-[var(--arcane-blue)]/30 mb-1">
                        <img
                          src={ddragon.getItemIcon(itemId)}
                          alt={ddragon.getItemName(itemId)}
                          className="w-full h-full object-cover"
                        />
                      </div>
                      <span className="text-xs text-[var(--text-muted)]">{index + 1}</span>
                    </div>
                  ))}
                </div>
              </div>

              {/* 4th Item Options */}
              {buildData.builds[0].fourthItemOptions.length > 0 && (
                <div>
                  <h3 className="text-sm font-medium text-[var(--text-secondary)] mb-3">4th Item Options</h3>
                  <div className="space-y-2">
                    {buildData.builds[0].fourthItemOptions.map((item) => (
                      <div
                        key={item.itemId}
                        className="flex items-center gap-3 p-2 bg-[var(--arcane-blue)]/20 rounded-lg"
                      >
                        <div className="w-10 h-10 rounded overflow-hidden border border-[var(--hextech-gold)]/20">
                          <img
                            src={ddragon.getItemIcon(item.itemId)}
                            alt={ddragon.getItemName(item.itemId)}
                            className="w-full h-full object-cover"
                          />
                        </div>
                        <span className="flex-1 text-[var(--text-primary)] text-sm">
                          {ddragon.getItemName(item.itemId)}
                        </span>
                        <span className={`text-sm font-medium ${getWinRateClass(item.winRate)}`}>
                          {item.winRate.toFixed(1)}%
                        </span>
                        <span className="text-xs text-[var(--text-muted)]">
                          {item.games.toLocaleString()}
                        </span>
                      </div>
                    ))}
                  </div>
                </div>
              )}

              {/* 5th Item Options */}
              {buildData.builds[0].fifthItemOptions.length > 0 && (
                <div>
                  <h3 className="text-sm font-medium text-[var(--text-secondary)] mb-3">5th Item Options</h3>
                  <div className="space-y-2">
                    {buildData.builds[0].fifthItemOptions.map((item) => (
                      <div
                        key={item.itemId}
                        className="flex items-center gap-3 p-2 bg-[var(--arcane-blue)]/20 rounded-lg"
                      >
                        <div className="w-10 h-10 rounded overflow-hidden border border-[var(--hextech-gold)]/20">
                          <img
                            src={ddragon.getItemIcon(item.itemId)}
                            alt={ddragon.getItemName(item.itemId)}
                            className="w-full h-full object-cover"
                          />
                        </div>
                        <span className="flex-1 text-[var(--text-primary)] text-sm">
                          {ddragon.getItemName(item.itemId)}
                        </span>
                        <span className={`text-sm font-medium ${getWinRateClass(item.winRate)}`}>
                          {item.winRate.toFixed(1)}%
                        </span>
                        <span className="text-xs text-[var(--text-muted)]">
                          {item.games.toLocaleString()}
                        </span>
                      </div>
                    ))}
                  </div>
                </div>
              )}
            </div>
          </div>
        )}

        {/* Matchups Section */}
        <div className="space-y-8">
          {/* Counters (Worst Matchups) */}
          <div className="hex-card rounded-xl overflow-hidden">
            <div className="bg-gradient-to-r from-[var(--deep-navy)] to-[var(--arcane-blue)] px-6 py-4 border-b border-[var(--hextech-gold)]/20">
              <h2 className="text-xl font-display font-semibold text-[var(--pale-gold)]">
                Counters (Hardest Matchups)
              </h2>
            </div>
            <div className="p-6">
              {counters.length === 0 ? (
                <p className="text-[var(--text-muted)] text-center py-4">No matchup data available</p>
              ) : (
                <div className="space-y-3">
                  {counters.map((matchup) => (
                    <Link
                      key={matchup.enemyChampionId}
                      href={`/stats/${matchup.enemyChampionId}?role=${selectedRole}`}
                      className="flex items-center gap-3 p-3 bg-[var(--arcane-blue)]/20 rounded-lg hover:bg-[var(--arcane-blue)]/40 transition-all group"
                    >
                      <div className="w-10 h-10 rounded-full overflow-hidden border-2 border-red-500/30">
                        <img
                          src={ddragon.getChampionIcon(matchup.enemyChampionId)}
                          alt={ddragon.getChampionName(matchup.enemyChampionId)}
                          className="w-full h-full object-cover"
                        />
                      </div>
                      <span className="flex-1 text-[var(--text-primary)] font-medium group-hover:text-[var(--pale-gold)] transition-colors">
                        {ddragon.getChampionName(matchup.enemyChampionId)}
                      </span>
                      <div className="text-right">
                        <div className="wr-low font-semibold">{matchup.winRate.toFixed(1)}%</div>
                        <div className="text-xs text-[var(--text-muted)]">
                          {matchup.matches.toLocaleString()} games
                        </div>
                      </div>
                    </Link>
                  ))}
                </div>
              )}
            </div>
          </div>

          {/* Best Matchups */}
          <div className="hex-card rounded-xl overflow-hidden">
            <div className="bg-gradient-to-r from-[var(--deep-navy)] to-[var(--arcane-blue)] px-6 py-4 border-b border-[var(--hextech-gold)]/20">
              <h2 className="text-xl font-display font-semibold text-[var(--pale-gold)]">
                Best Matchups
              </h2>
            </div>
            <div className="p-6">
              {bestMatchups.length === 0 ? (
                <p className="text-[var(--text-muted)] text-center py-4">No matchup data available</p>
              ) : (
                <div className="space-y-3">
                  {bestMatchups.map((matchup) => (
                    <Link
                      key={matchup.enemyChampionId}
                      href={`/stats/${matchup.enemyChampionId}?role=${selectedRole}`}
                      className="flex items-center gap-3 p-3 bg-[var(--arcane-blue)]/20 rounded-lg hover:bg-[var(--arcane-blue)]/40 transition-all group"
                    >
                      <div className="w-10 h-10 rounded-full overflow-hidden border-2 border-green-500/30">
                        <img
                          src={ddragon.getChampionIcon(matchup.enemyChampionId)}
                          alt={ddragon.getChampionName(matchup.enemyChampionId)}
                          className="w-full h-full object-cover"
                        />
                      </div>
                      <span className="flex-1 text-[var(--text-primary)] font-medium group-hover:text-[var(--pale-gold)] transition-colors">
                        {ddragon.getChampionName(matchup.enemyChampionId)}
                      </span>
                      <div className="text-right">
                        <div className="wr-high font-semibold">{matchup.winRate.toFixed(1)}%</div>
                        <div className="text-xs text-[var(--text-muted)]">
                          {matchup.matches.toLocaleString()} games
                        </div>
                      </div>
                    </Link>
                  ))}
                </div>
              )}
            </div>
          </div>
        </div>
      </div>

      {/* Patch Info */}
      {statsInfo.patch && (
        <p className="text-center text-[var(--text-muted)] text-sm mt-12">
          Data from Patch {statsInfo.patch}
        </p>
      )}
    </div>
  );
}
