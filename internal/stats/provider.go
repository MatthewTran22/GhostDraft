package stats

import (
	"database/sql"
	"fmt"

	"ghostdraft/internal/data"
)

// ItemOption holds item ID with win rate
type ItemOption struct {
	ItemID  int
	WinRate float64
	Games   int
}

// BuildPath represents a single build path
type BuildPath struct {
	Name              string
	WinRate           float64
	Games             int
	StartingItems     []int
	CoreItems         []int
	FourthItemOptions []ItemOption
	FifthItemOptions  []ItemOption
	SixthItemOptions  []ItemOption
}

// BuildData holds champion build information
type BuildData struct {
	ChampionID   int
	ChampionName string
	Role         string
	Builds       []BuildPath
}

// Provider fetches build data from our SQLite database
type Provider struct {
	db           *sql.DB
	currentPatch string
}

// ItemStat represents aggregated item statistics
type ItemStat struct {
	ItemID   int
	Wins     int
	Matches  int
	WinRate  float64
	PickRate float64
}

// MatchupStat represents a champion matchup statistic
type MatchupStat struct {
	EnemyChampionID int
	Wins            int
	Matches         int
	WinRate         float64
}

// ChampionWinRate holds champion win rate data for meta display
type ChampionWinRate struct {
	ChampionID int
	Wins       int
	Matches    int
	WinRate    float64
	PickRate   float64
}

// NewProvider creates a new stats provider from a StatsDB
func NewProvider(statsDB *data.StatsDB) (*Provider, error) {
	return &Provider{
		db:           statsDB.GetDB(),
		currentPatch: statsDB.GetCurrentPatch(),
	}, nil
}

// Close is a no-op since the StatsDB owns the connection
func (p *Provider) Close() {
	// Connection owned by StatsDB
}

// FetchPatch gets the latest patch from our database
func (p *Provider) FetchPatch() error {
	var patch string
	err := p.db.QueryRow(`
		SELECT patch FROM champion_stats
		ORDER BY patch DESC
		LIMIT 1
	`).Scan(&patch)

	if err != nil {
		return fmt.Errorf("failed to get patch: %w", err)
	}

	p.currentPatch = patch
	fmt.Printf("[Stats] Using patch: %s\n", patch)
	return nil
}

// GetPatch returns the current patch
func (p *Provider) GetPatch() string {
	return p.currentPatch
}

// roleToPosition converts role names to database team_position values
func roleToPosition(role string) string {
	switch role {
	case "top":
		return "TOP"
	case "jungle":
		return "JUNGLE"
	case "middle", "mid":
		return "MIDDLE"
	case "bottom", "adc":
		return "BOTTOM"
	case "utility", "support":
		return "UTILITY"
	default:
		return "MIDDLE"
	}
}

// FetchChampionData gets build data for a champion from our database
func (p *Provider) FetchChampionData(championID int, championName string, role string) (*BuildData, error) {
	position := roleToPosition(role)

	// Get total games for this champion/position (aggregate across all patches)
	var totalGames int
	err := p.db.QueryRow(`
		SELECT COALESCE(SUM(matches), 0) FROM champion_stats
		WHERE champion_id = ? AND team_position = ?
	`, championID, position).Scan(&totalGames)

	if err != nil || totalGames == 0 {
		return nil, fmt.Errorf("no data for champion %d in position %s", championID, position)
	}

	// Build the response using slot-based data
	build, err := p.constructBuildPathFromSlots(championID, position, totalGames)
	if err != nil {
		return nil, err
	}

	return &BuildData{
		ChampionID:   championID,
		ChampionName: championName,
		Role:         role,
		Builds:       []BuildPath{build},
	}, nil
}

// constructBuildPathFromSlots creates a build path using item slot data
func (p *Provider) constructBuildPathFromSlots(championID int, position string, totalGames int) (BuildPath, error) {
	// Track excluded items (already used in build)
	excluded := make(map[int]bool)

	// Query items for each slot, ordered by matches (popularity)
	// Excludes boots and any items in the excluded map
	getSlotItems := func(slot int, limit int, excludeBoots bool) ([]ItemOption, error) {
		rows, err := p.db.Query(`
			SELECT item_id, SUM(wins) as wins, SUM(matches) as matches
			FROM champion_item_slots
			WHERE champion_id = ? AND team_position = ? AND build_slot = ?
			GROUP BY item_id
			ORDER BY SUM(matches) DESC
		`, championID, position, slot)
		if err != nil {
			return nil, err
		}
		defer rows.Close()

		var items []ItemOption
		for rows.Next() {
			var itemID, wins, matches int
			if err := rows.Scan(&itemID, &wins, &matches); err != nil {
				continue
			}
			if matches > 0 {
				// Skip excluded items (duplicates)
				if excluded[itemID] {
					continue
				}
				// Skip boots if requested
				if excludeBoots && isBootsItem(itemID) {
					continue
				}
				// Skip starting items
				if isStartingItem(itemID) {
					continue
				}
				items = append(items, ItemOption{
					ItemID:  itemID,
					WinRate: float64(wins) / float64(matches) * 100,
					Games:   matches,
				})
				if len(items) >= limit {
					break
				}
			}
		}
		return items, nil
	}

	// Get best boots across all slots
	var bestBoots int
	bootsRow := p.db.QueryRow(`
		SELECT item_id
		FROM champion_item_slots
		WHERE champion_id = ? AND team_position = ?
		AND item_id IN (3006, 3009, 3020, 3047, 3111, 3117, 3158)
		GROUP BY item_id
		ORDER BY SUM(matches) DESC
		LIMIT 1
	`, championID, position)
	bootsRow.Scan(&bestBoots)

	// Get 2 core items (slots 1, 2, 3 - excluding boots and duplicates)
	var coreItemIDs []int
	var winRate float64
	for slot := 1; slot <= 3; slot++ {
		if len(coreItemIDs) >= 2 {
			break
		}
		items, err := getSlotItems(slot, 1, true) // exclude boots
		if err != nil {
			continue
		}
		if len(items) > 0 {
			coreItemIDs = append(coreItemIDs, items[0].ItemID)
			excluded[items[0].ItemID] = true
			if len(coreItemIDs) == 1 {
				winRate = items[0].WinRate
			}
		}
	}

	// Add best boots to core items
	if bestBoots > 0 {
		coreItemIDs = append(coreItemIDs, bestBoots)
		excluded[bestBoots] = true
	}

	// Mark all boots as excluded for later slots
	for _, bootsID := range []int{3006, 3009, 3020, 3047, 3111, 3117, 3158} {
		excluded[bootsID] = true
	}

	// Get 4th, 5th, 6th item options (3 choices each, excluding core and boots)
	fourthItems, _ := getSlotItems(4, 3, true)
	fifthItems, _ := getSlotItems(5, 3, true)
	sixthItems, _ := getSlotItems(6, 3, true)

	return BuildPath{
		Name:              "Recommended Build",
		WinRate:           winRate,
		Games:             totalGames,
		StartingItems:     nil,
		CoreItems:         coreItemIDs,
		FourthItemOptions: fourthItems,
		FifthItemOptions:  fifthItems,
		SixthItemOptions:  sixthItems,
	}, nil
}

// toItemOptions converts ItemStats to ItemOptions
func (p *Provider) toItemOptions(items []ItemStat, start, count int) []ItemOption {
	var options []ItemOption
	end := start + count
	if end > len(items) {
		end = len(items)
	}

	for i := start; i < end; i++ {
		options = append(options, ItemOption{
			ItemID:  items[i].ItemID,
			WinRate: items[i].WinRate,
			Games:   items[i].Matches,
		})
	}
	return options
}

// isBootsItem checks if an item is boots
func isBootsItem(itemID int) bool {
	boots := map[int]bool{
		3006: true, // Berserker's Greaves
		3009: true, // Boots of Swiftness
		3020: true, // Sorcerer's Shoes
		3047: true, // Plated Steelcaps
		3111: true, // Mercury's Treads
		3117: true, // Mobility Boots
		3158: true, // Ionian Boots of Lucidity
	}
	return boots[itemID]
}

// isStartingItem checks if an item is a starting item
func isStartingItem(itemID int) bool {
	starters := map[int]bool{
		1054: true, // Doran's Shield
		1055: true, // Doran's Blade
		1056: true, // Doran's Ring
		1082: true, // Dark Seal
		1083: true, // Cull
		1101: true, // Scorchclaw Pup
		1102: true, // Gustwalker Hatchling
		1103: true, // Mosstomper Seedling
		2003: true, // Health Potion
		2031: true, // Refillable Potion
		2033: true, // Corrupting Potion
		3070: true, // Tear of the Goddess
		3850: true, // Spellthief's Edge
		3851: true, // Frostfang
		3854: true, // Steel Shoulderguards
		3855: true, // Runesteel Spaulders
		3858: true, // Relic Shield
		3859: true, // Targon's Buckler
		3862: true, // Spectral Sickle
		3863: true, // Harrowing Crescent
		1036: true, // Long Sword
		1052: true, // Amplifying Tome
		1058: true, // Needlessly Large Rod
	}
	return starters[itemID]
}

// HasData checks if we have data for a champion
func (p *Provider) HasData(championID int, role string) bool {
	position := roleToPosition(role)

	var count int
	err := p.db.QueryRow(`
		SELECT COUNT(*) FROM champion_items
		WHERE champion_id = ? AND team_position = ?
	`, championID, position).Scan(&count)

	return err == nil && count > 0
}

// FetchMatchup returns the win rate for a specific champion vs enemy matchup
func (p *Provider) FetchMatchup(championID int, enemyChampionID int, role string) (*MatchupStat, error) {
	position := roleToPosition(role)

	var m MatchupStat
	m.EnemyChampionID = enemyChampionID

	// Aggregate across all patches
	err := p.db.QueryRow(`
		SELECT COALESCE(SUM(wins), 0), COALESCE(SUM(matches), 0)
		FROM champion_matchups
		WHERE champion_id = ? AND team_position = ? AND enemy_champion_id = ?
	`, championID, position, enemyChampionID).Scan(&m.Wins, &m.Matches)

	if err != nil || m.Matches == 0 {
		return nil, fmt.Errorf("no matchup data for %d vs %d", championID, enemyChampionID)
	}

	m.WinRate = float64(m.Wins) / float64(m.Matches) * 100
	return &m, nil
}

// FetchAllMatchups returns all matchup data for a champion in a role
func (p *Provider) FetchAllMatchups(championID int, role string) ([]MatchupStat, error) {
	position := roleToPosition(role)

	// Aggregate across all patches
	rows, err := p.db.Query(`
		SELECT enemy_champion_id, SUM(wins) as wins, SUM(matches) as matches
		FROM champion_matchups
		WHERE champion_id = ? AND team_position = ?
		GROUP BY enemy_champion_id
		ORDER BY SUM(matches) DESC
	`, championID, position)

	if err != nil {
		return nil, fmt.Errorf("failed to query matchups: %w", err)
	}
	defer rows.Close()

	var matchups []MatchupStat
	for rows.Next() {
		var m MatchupStat
		if err := rows.Scan(&m.EnemyChampionID, &m.Wins, &m.Matches); err != nil {
			continue
		}
		if m.Matches > 0 {
			m.WinRate = float64(m.Wins) / float64(m.Matches) * 100
		}
		matchups = append(matchups, m)
	}

	return matchups, nil
}

// FetchCounterMatchups returns the champions that counter the specified champion
// (i.e., matchups where the specified champion has the lowest win rate)
func (p *Provider) FetchCounterMatchups(championID int, role string, limit int) ([]MatchupStat, error) {
	position := roleToPosition(role)

	if limit <= 0 {
		limit = 10
	}

	// Query matchups ordered by lowest win rate (hardest counters first)
	// Only include matchups where win rate < 49% (true counters)
	rows, err := p.db.Query(`
		SELECT enemy_champion_id, SUM(wins) as wins, SUM(matches) as matches
		FROM champion_matchups
		WHERE champion_id = ? AND team_position = ?
		GROUP BY enemy_champion_id
		HAVING SUM(matches) >= 10
		   AND (CAST(SUM(wins) AS REAL) / CAST(SUM(matches) AS REAL)) < 0.49
		ORDER BY (CAST(SUM(wins) AS REAL) / CAST(SUM(matches) AS REAL)) ASC
		LIMIT ?
	`, championID, position, limit)

	if err != nil {
		return nil, fmt.Errorf("failed to query matchups: %w", err)
	}
	defer rows.Close()

	var matchups []MatchupStat
	for rows.Next() {
		var m MatchupStat
		if err := rows.Scan(&m.EnemyChampionID, &m.Wins, &m.Matches); err != nil {
			continue
		}
		if m.Matches > 0 {
			m.WinRate = float64(m.Wins) / float64(m.Matches) * 100
		}
		matchups = append(matchups, m)
	}

	return matchups, nil
}

// FetchCounterPicks returns champions that counter a specific enemy champion in a role
// (i.e., champions with high win rate against the enemy)
func (p *Provider) FetchCounterPicks(enemyChampionID int, role string, limit int) ([]MatchupStat, error) {
	position := roleToPosition(role)

	if limit <= 0 {
		limit = 5
	}

	// Query champions that have high win rate against this enemy
	// We flip the query - find champions where they beat the enemy
	rows, err := p.db.Query(`
		SELECT champion_id, SUM(wins) as wins, SUM(matches) as matches
		FROM champion_matchups
		WHERE enemy_champion_id = ? AND team_position = ?
		GROUP BY champion_id
		HAVING SUM(matches) >= 10
		   AND (CAST(SUM(wins) AS REAL) / CAST(SUM(matches) AS REAL)) > 0.51
		ORDER BY (CAST(SUM(wins) AS REAL) / CAST(SUM(matches) AS REAL)) DESC
		LIMIT ?
	`, enemyChampionID, position, limit)

	if err != nil {
		return nil, fmt.Errorf("failed to query counter picks: %w", err)
	}
	defer rows.Close()

	var matchups []MatchupStat
	for rows.Next() {
		var m MatchupStat
		m.EnemyChampionID = enemyChampionID
		var champID int
		if err := rows.Scan(&champID, &m.Wins, &m.Matches); err != nil {
			continue
		}
		if m.Matches > 0 {
			m.WinRate = float64(m.Wins) / float64(m.Matches) * 100
		}
		// Store the counter pick champion ID in EnemyChampionID field (repurposed)
		m.EnemyChampionID = champID
		matchups = append(matchups, m)
	}

	return matchups, nil
}

// FetchTopChampionsByRole returns the top N champions by win rate for a given role
func (p *Provider) FetchTopChampionsByRole(role string, limit int) ([]ChampionWinRate, error) {
	position := roleToPosition(role)

	if limit <= 0 {
		limit = 5
	}

	// Get total games for this position to calculate pick rate
	var totalGames int
	err := p.db.QueryRow(`
		SELECT COALESCE(SUM(matches), 0) FROM champion_stats
		WHERE team_position = ?
	`, position).Scan(&totalGames)
	if err != nil {
		totalGames = 0
	}

	// Query champions ordered by highest win rate
	// Aggregate across all patches, include champions with at least 100 games
	rows, err := p.db.Query(`
		SELECT champion_id, SUM(wins) as wins, SUM(matches) as matches
		FROM champion_stats
		WHERE team_position = ?
		GROUP BY champion_id
		HAVING SUM(matches) >= 100
		ORDER BY (CAST(SUM(wins) AS REAL) / CAST(SUM(matches) AS REAL)) DESC
		LIMIT ?
	`, position, limit)

	if err != nil {
		return nil, fmt.Errorf("failed to query top champions: %w", err)
	}
	defer rows.Close()

	var champions []ChampionWinRate
	for rows.Next() {
		var c ChampionWinRate
		if err := rows.Scan(&c.ChampionID, &c.Wins, &c.Matches); err != nil {
			continue
		}
		if c.Matches > 0 {
			c.WinRate = float64(c.Wins) / float64(c.Matches) * 100
			if totalGames > 0 {
				c.PickRate = float64(c.Matches) / float64(totalGames) * 100
			}
		}
		champions = append(champions, c)
	}

	return champions, nil
}

// FetchAllRolesTopChampions returns top N champions for all 5 roles
func (p *Provider) FetchAllRolesTopChampions(limit int) (map[string][]ChampionWinRate, error) {
	roles := []string{"top", "jungle", "middle", "bottom", "utility"}
	result := make(map[string][]ChampionWinRate)

	for _, role := range roles {
		champs, err := p.FetchTopChampionsByRole(role, limit)
		if err != nil {
			result[role] = []ChampionWinRate{}
			continue
		}
		result[role] = champs
	}

	return result, nil
}
