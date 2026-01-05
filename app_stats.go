package main

import (
	"fmt"
	"os"

	"ghostdraft/internal/data"
	"ghostdraft/internal/lcu"
	"ghostdraft/internal/stats"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// MetaChampion represents a champion in the meta list
type MetaChampion struct {
	ChampionID   int     `json:"championId"`
	ChampionName string  `json:"championName"`
	IconURL      string  `json:"iconURL"`
	WinRate      float64 `json:"winRate"`
	PickRate     float64 `json:"pickRate"`
	Games        int     `json:"games"`
}

// MetaData represents the top champions for all roles
type MetaData struct {
	Patch   string                    `json:"patch"`
	HasData bool                      `json:"hasData"`
	Roles   map[string][]MetaChampion `json:"roles"`
}

// fetchAndEmitBuild fetches matchup data from our database and emits it to frontend
func (a *App) fetchAndEmitBuild(championID int, championName string, role string, enemyChampionIDs []int) {
	fmt.Printf("Fetching matchup for %s (%s) vs %d enemies...\n", championName, role, len(enemyChampionIDs))

	patch := ""
	if a.statsProvider != nil {
		patch = a.statsProvider.GetPatch()
	}

	if len(enemyChampionIDs) == 0 {
		runtime.EventsEmit(a.ctx, "build:update", map[string]interface{}{
			"hasBuild":     true,
			"championName": championName,
			"role":         role,
			"winRate":      "-",
			"winRateLabel": "Waiting for enemy...",
			"patch":        patch,
		})
		fmt.Printf("No enemies detected yet for %s\n", championName)
		return
	}

	if a.statsProvider == nil {
		runtime.EventsEmit(a.ctx, "build:update", map[string]interface{}{
			"hasBuild": false,
			"error":    "Stats provider not available",
		})
		fmt.Println("Stats provider not available for matchups")
		return
	}

	// Fetch our matchups - this gives us all enemies we face in our role
	matchups, err := a.statsProvider.FetchAllMatchups(championID, role)
	if err != nil {
		runtime.EventsEmit(a.ctx, "build:update", map[string]interface{}{
			"hasBuild": false,
			"error":    err.Error(),
		})
		fmt.Printf("Failed to fetch matchups: %v\n", err)
		return
	}

	// Find enemy with highest game count in matchup data (likely lane opponent)
	var laneOpponentID int
	var matchupWR float64
	var matchupGames int
	for _, enemyID := range enemyChampionIDs {
		for _, m := range matchups {
			if m.EnemyChampionID == enemyID && m.Matches > matchupGames {
				laneOpponentID = enemyID
				matchupWR = m.WinRate
				matchupGames = m.Matches
			}
		}
	}
	if laneOpponentID > 0 {
		fmt.Printf("Lane opponent (highest games): %d (%.1f%% WR, %d games)\n", laneOpponentID, matchupWR, matchupGames)
	}

	if laneOpponentID == 0 {
		runtime.EventsEmit(a.ctx, "build:update", map[string]interface{}{
			"hasBuild":     true,
			"championName": championName,
			"role":         role,
			"winRate":      "-",
			"winRateLabel": "No lane opponent found",
			"patch":        patch,
		})
		fmt.Printf("No lane opponent found in matchup data for %s\n", championName)
		return
	}

	enemyName := a.champions.GetName(laneOpponentID)

	// Determine matchup status: winning (>51%), losing (<49%), even (49-51%)
	var matchupStatus string
	if matchupWR >= 51.0 {
		matchupStatus = "winning"
	} else if matchupWR <= 49.0 {
		matchupStatus = "losing"
	} else {
		matchupStatus = "even"
	}

	fmt.Printf("Matchup: %s vs %s = %.1f%% (%s, %d games)\n", championName, enemyName, matchupWR, matchupStatus, matchupGames)
	runtime.EventsEmit(a.ctx, "build:update", map[string]interface{}{
		"hasBuild":      true,
		"championName":  championName,
		"role":          role,
		"winRate":       fmt.Sprintf("%.1f%%", matchupWR),
		"winRateLabel":  fmt.Sprintf("vs %s", enemyName),
		"enemyName":     enemyName,
		"matchupStatus": matchupStatus,
		"patch":         patch,
	})
}

// fetchAndEmitCounterPicks fetches champions that counter the enemy laner
func (a *App) fetchAndEmitCounterPicks(enemyChampionID int, role string) {
	enemyName := a.champions.GetName(enemyChampionID)
	fmt.Printf("Fetching counter picks vs %s (%s)...\n", enemyName, role)

	if a.statsProvider == nil {
		fmt.Println("Stats provider not available for counter picks")
		runtime.EventsEmit(a.ctx, "counterpicks:update", map[string]interface{}{
			"hasData": false,
		})
		return
	}

	counterPicks, err := a.statsProvider.FetchCounterPicks(enemyChampionID, role, 6)
	if err != nil || len(counterPicks) == 0 {
		fmt.Printf("No counter pick data vs %s: %v\n", enemyName, err)
		runtime.EventsEmit(a.ctx, "counterpicks:update", map[string]interface{}{
			"hasData":   true,
			"enemyName": enemyName,
			"enemyIcon": a.champions.GetIconURL(enemyChampionID),
			"picks":     []map[string]interface{}{},
		})
		return
	}

	// Convert to frontend format
	var pickList []map[string]interface{}
	for _, m := range counterPicks {
		champName := a.champions.GetName(m.EnemyChampionID)
		pickList = append(pickList, map[string]interface{}{
			"championID":   m.EnemyChampionID,
			"championName": champName,
			"iconURL":      a.champions.GetIconURL(m.EnemyChampionID),
			"winRate":      m.WinRate,
			"games":        m.Matches,
		})
	}

	fmt.Printf("Counter picks vs %s: ", enemyName)
	for _, p := range pickList {
		fmt.Printf("%s (%.1f%%) ", p["championName"], p["winRate"])
	}
	fmt.Println()

	runtime.EventsEmit(a.ctx, "counterpicks:update", map[string]interface{}{
		"hasData":   true,
		"enemyName": enemyName,
		"enemyIcon": a.champions.GetIconURL(enemyChampionID),
		"picks":     pickList,
	})
}

// fetchAndEmitRecommendedBans fetches hardest counters and emits as recommended bans
func (a *App) fetchAndEmitRecommendedBans(championID int, role string) {
	championName := a.champions.GetName(championID)
	fmt.Printf("Fetching recommended bans for %s (%s)...\n", championName, role)

	// Use our stats provider for counter matchups
	if a.statsProvider == nil {
		fmt.Println("Stats provider not available for bans")
		runtime.EventsEmit(a.ctx, "bans:update", map[string]interface{}{
			"hasBans":      true,
			"championName": championName,
			"role":         role,
			"bans":         []map[string]interface{}{},
			"noData":       true,
		})
		return
	}

	matchups, err := a.statsProvider.FetchCounterMatchups(championID, role, 5)
	if err != nil || len(matchups) == 0 {
		fmt.Printf("No matchup data for %s %s: %v\n", championName, role, err)
		runtime.EventsEmit(a.ctx, "bans:update", map[string]interface{}{
			"hasBans":      true,
			"championName": championName,
			"role":         role,
			"bans":         []map[string]interface{}{},
			"noData":       true,
		})
		return
	}

	// Convert to frontend format
	var banList []map[string]interface{}
	for _, m := range matchups {
		enemyName := a.champions.GetName(m.EnemyChampionID)
		damageType := "Unknown"
		if a.championDB != nil {
			damageType = a.championDB.GetDamageType(enemyName)
		}
		banList = append(banList, map[string]interface{}{
			"championID":   m.EnemyChampionID,
			"championName": enemyName,
			"iconURL":      a.champions.GetIconURL(m.EnemyChampionID),
			"damageType":   damageType,
			"winRate":      m.WinRate,
			"games":        m.Matches,
		})
	}

	fmt.Printf("Counter matchups for %s: ", championName)
	for _, b := range banList {
		fmt.Printf("%s (%.1f%%) ", b["championName"], b["winRate"])
	}
	fmt.Println()

	runtime.EventsEmit(a.ctx, "bans:update", map[string]interface{}{
		"hasBans":      true,
		"championName": championName,
		"role":         role,
		"bans":         banList,
	})
}

// fetchAndEmitItems fetches item build from our stats database and emits to frontend
func (a *App) fetchAndEmitItems(championID int, championName string, role string) {
	fmt.Printf("Fetching items for %s (%s)...\n", championName, role)

	if a.statsProvider == nil {
		fmt.Println("Stats provider not available")
		runtime.EventsEmit(a.ctx, "items:update", map[string]interface{}{
			"hasItems": false,
		})
		return
	}

	buildData, err := a.statsProvider.FetchChampionData(championID, championName, role)
	if err != nil {
		fmt.Printf("No data for %s: %v\n", championName, err)
		runtime.EventsEmit(a.ctx, "items:update", map[string]interface{}{
			"hasItems": false,
		})
		return
	}

	// Helper to convert item IDs to frontend format
	convertItems := func(itemIDs []int) []map[string]interface{} {
		var result []map[string]interface{}
		for _, itemID := range itemIDs {
			result = append(result, map[string]interface{}{
				"id":      itemID,
				"name":    a.items.GetName(itemID),
				"iconURL": a.items.GetIconURL(itemID),
			})
		}
		return result
	}

	// Helper to convert item options with win rates
	convertItemOptions := func(options []stats.ItemOption) []map[string]interface{} {
		var result []map[string]interface{}
		for _, opt := range options {
			result = append(result, map[string]interface{}{
				"id":      opt.ItemID,
				"name":    a.items.GetName(opt.ItemID),
				"iconURL": a.items.GetIconURL(opt.ItemID),
				"winRate": opt.WinRate,
				"games":   opt.Games,
			})
		}
		return result
	}

	// Convert all build paths
	var builds []map[string]interface{}
	for _, build := range buildData.Builds {
		// Name the build after the first core item
		buildName := "Build"
		if len(build.CoreItems) > 0 {
			buildName = a.items.GetName(build.CoreItems[0])
		}

		builds = append(builds, map[string]interface{}{
			"name":          buildName,
			"winRate":       build.WinRate,
			"games":         build.Games,
			"startingItems": convertItems(build.StartingItems),
			"coreItems":     convertItems(build.CoreItems),
			"fourthItems":   convertItemOptions(build.FourthItemOptions),
			"fifthItems":    convertItemOptions(build.FifthItemOptions),
			"sixthItems":    convertItemOptions(build.SixthItemOptions),
		})
	}

	fmt.Printf("Found %d build paths for %s\n", len(builds), championName)

	runtime.EventsEmit(a.ctx, "items:update", map[string]interface{}{
		"hasItems":     true,
		"championName": championName,
		"role":         role,
		"builds":       builds,
	})
}

// ForceStatsUpdate forces a redownload of stats data
func (a *App) ForceStatsUpdate() string {
	if a.statsDB == nil {
		return "Stats database not initialized"
	}

	manifestURL := os.Getenv("STATS_MANIFEST_URL")
	if manifestURL == "" {
		manifestURL = data.DefaultManifestURL
	}

	if err := a.statsDB.ForceUpdate(manifestURL); err != nil {
		return fmt.Sprintf("Update failed: %v", err)
	}

	// Recreate stats provider with new data
	if a.statsDB.HasData() {
		provider, err := stats.NewProvider(a.statsDB)
		if err != nil {
			return fmt.Sprintf("Provider creation failed: %v", err)
		}
		if provider.GetPatch() == "" {
			provider.FetchPatch()
		}
		a.statsProvider = provider
		return fmt.Sprintf("Updated to %s", provider.GetPatch())
	}

	return "Update completed but no data available"
}

// GetMetaChampions returns the top 5 champions by win rate for each role
func (a *App) GetMetaChampions() MetaData {
	result := MetaData{
		HasData: false,
		Roles:   make(map[string][]MetaChampion),
	}

	if a.statsProvider == nil {
		return result
	}

	result.Patch = a.statsProvider.GetPatch()

	roleData, err := a.statsProvider.FetchAllRolesTopChampions(5)
	if err != nil {
		return result
	}

	for role, champs := range roleData {
		var metaChamps []MetaChampion
		for _, c := range champs {
			name := a.champions.GetName(c.ChampionID)
			icon := a.champions.GetIconURL(c.ChampionID)
			metaChamps = append(metaChamps, MetaChampion{
				ChampionID:   c.ChampionID,
				ChampionName: name,
				IconURL:      icon,
				WinRate:      c.WinRate,
				PickRate:     c.PickRate,
				Games:        c.Matches,
			})
		}
		result.Roles[role] = metaChamps
	}

	result.HasData = true
	return result
}

// GetPersonalStats returns aggregated personal stats from recent match history
func (a *App) GetPersonalStats() *lcu.PersonalStats {
	emptyStats := &lcu.PersonalStats{HasData: false}

	if !a.lcuClient.IsConnected() {
		return emptyStats
	}

	history, err := a.lcuClient.FetchMatchHistory(20)
	if err != nil {
		fmt.Printf("Failed to fetch match history: %v\n", err)
		return emptyStats
	}

	return lcu.CalculatePersonalStats(history, a.champions)
}

// ChampionDetailItem represents an item in a build
type ChampionDetailItem struct {
	ItemID  int     `json:"itemId"`
	Name    string  `json:"name"`
	IconURL string  `json:"iconURL"`
	WinRate float64 `json:"winRate"`
	Games   int     `json:"games"`
}

// ChampionDetailMatchup represents a matchup
type ChampionDetailMatchup struct {
	ChampionID   int     `json:"championId"`
	ChampionName string  `json:"championName"`
	IconURL      string  `json:"iconURL"`
	WinRate      float64 `json:"winRate"`
	Games        int     `json:"games"`
}

// ChampionDetails represents detailed info for a champion
type ChampionDetails struct {
	HasData       bool                    `json:"hasData"`
	ChampionID    int                     `json:"championId"`
	ChampionName  string                  `json:"championName"`
	Role          string                  `json:"role"`
	CoreItems     []ChampionDetailItem    `json:"coreItems"`
	FourthItems   []ChampionDetailItem    `json:"fourthItems"`
	FifthItems    []ChampionDetailItem    `json:"fifthItems"`
	SixthItems    []ChampionDetailItem    `json:"sixthItems"`
	Counters      []ChampionDetailMatchup `json:"counters"`
	GoodMatchups  []ChampionDetailMatchup `json:"goodMatchups"`
}

// BuildItem represents an item in a build
type BuildItem struct {
	ID      int     `json:"id"`
	Name    string  `json:"name"`
	IconURL string  `json:"iconURL"`
	WinRate float64 `json:"winRate,omitempty"`
	Games   int     `json:"games,omitempty"`
}

// BuildPath represents a single build path
type BuildPath struct {
	Name          string      `json:"name"`
	WinRate       float64     `json:"winRate"`
	Games         int         `json:"games"`
	StartingItems []BuildItem `json:"startingItems"`
	CoreItems     []BuildItem `json:"coreItems"`
	FourthItems   []BuildItem `json:"fourthItems"`
	FifthItems    []BuildItem `json:"fifthItems"`
	SixthItems    []BuildItem `json:"sixthItems"`
}

// ChampionBuildData represents build data for a champion
type ChampionBuildData struct {
	HasItems     bool        `json:"hasItems"`
	ChampionName string      `json:"championName"`
	ChampionID   int         `json:"championId"`
	Role         string      `json:"role"`
	IconURL      string      `json:"iconURL"`
	SplashURL    string      `json:"splashURL"`
	Builds       []BuildPath `json:"builds"`
}

// GetChampionBuild returns build data for a champion in the same format as items:update
func (a *App) GetChampionBuild(championID int, role string) ChampionBuildData {
	result := ChampionBuildData{
		HasItems:   false,
		ChampionID: championID,
		Builds:     []BuildPath{},
	}

	champName := a.champions.GetName(championID)
	result.ChampionName = champName
	result.Role = role
	result.IconURL = a.champions.GetIconURL(championID)
	result.SplashURL = a.champions.GetSplashURL(championID)

	if a.statsProvider == nil {
		return result
	}

	buildData, err := a.statsProvider.FetchChampionData(championID, champName, role)
	if err != nil || buildData == nil || len(buildData.Builds) == 0 {
		return result
	}

	result.HasItems = true

	// Helper to convert item IDs to BuildItem
	convertItems := func(itemIDs []int) []BuildItem {
		var items []BuildItem
		for _, itemID := range itemIDs {
			items = append(items, BuildItem{
				ID:      itemID,
				Name:    a.items.GetName(itemID),
				IconURL: a.items.GetIconURL(itemID),
			})
		}
		return items
	}

	// Helper to convert item options with win rates
	convertItemOptions := func(options []stats.ItemOption) []BuildItem {
		var items []BuildItem
		for _, opt := range options {
			items = append(items, BuildItem{
				ID:      opt.ItemID,
				Name:    a.items.GetName(opt.ItemID),
				IconURL: a.items.GetIconURL(opt.ItemID),
				WinRate: opt.WinRate,
				Games:   opt.Games,
			})
		}
		return items
	}

	// Convert all build paths
	for _, build := range buildData.Builds {
		buildName := "Build"
		if len(build.CoreItems) > 0 {
			buildName = a.items.GetName(build.CoreItems[0])
		}

		result.Builds = append(result.Builds, BuildPath{
			Name:          buildName,
			WinRate:       build.WinRate,
			Games:         build.Games,
			StartingItems: convertItems(build.StartingItems),
			CoreItems:     convertItems(build.CoreItems),
			FourthItems:   convertItemOptions(build.FourthItemOptions),
			FifthItems:    convertItemOptions(build.FifthItemOptions),
			SixthItems:    convertItemOptions(build.SixthItemOptions),
		})
	}

	return result
}

// GetChampionDetails returns detailed build and matchup info for a champion
func (a *App) GetChampionDetails(championID int, role string) ChampionDetails {
	result := ChampionDetails{
		HasData:      false,
		ChampionID:   championID,
		Role:         role,
		CoreItems:    []ChampionDetailItem{},
		FourthItems:  []ChampionDetailItem{},
		FifthItems:   []ChampionDetailItem{},
		SixthItems:   []ChampionDetailItem{},
		Counters:     []ChampionDetailMatchup{},
		GoodMatchups: []ChampionDetailMatchup{},
	}

	if a.statsProvider == nil {
		return result
	}

	champName := a.champions.GetName(championID)
	result.ChampionName = champName

	// Fetch build data
	buildData, err := a.statsProvider.FetchChampionData(championID, champName, role)
	if err == nil && buildData != nil && len(buildData.Builds) > 0 {
		result.HasData = true
		build := buildData.Builds[0]

		// Core items
		for _, itemID := range build.CoreItems {
			result.CoreItems = append(result.CoreItems, ChampionDetailItem{
				ItemID:  itemID,
				Name:    a.items.GetName(itemID),
				IconURL: a.items.GetIconURL(itemID),
			})
		}

		// 4th item options
		for _, opt := range build.FourthItemOptions[:min(3, len(build.FourthItemOptions))] {
			result.FourthItems = append(result.FourthItems, ChampionDetailItem{
				ItemID:  opt.ItemID,
				Name:    a.items.GetName(opt.ItemID),
				IconURL: a.items.GetIconURL(opt.ItemID),
				WinRate: opt.WinRate,
				Games:   opt.Games,
			})
		}

		// 5th item options
		for _, opt := range build.FifthItemOptions[:min(3, len(build.FifthItemOptions))] {
			result.FifthItems = append(result.FifthItems, ChampionDetailItem{
				ItemID:  opt.ItemID,
				Name:    a.items.GetName(opt.ItemID),
				IconURL: a.items.GetIconURL(opt.ItemID),
				WinRate: opt.WinRate,
				Games:   opt.Games,
			})
		}

		// 6th item options
		for _, opt := range build.SixthItemOptions[:min(3, len(build.SixthItemOptions))] {
			result.SixthItems = append(result.SixthItems, ChampionDetailItem{
				ItemID:  opt.ItemID,
				Name:    a.items.GetName(opt.ItemID),
				IconURL: a.items.GetIconURL(opt.ItemID),
				WinRate: opt.WinRate,
				Games:   opt.Games,
			})
		}
	}

	// Fetch counters (champions that beat you) - separate from allMatchups
	counters, err := a.statsProvider.FetchCounterMatchups(championID, role, 6)
	if err != nil {
		fmt.Printf("Failed to fetch counters for %s: %v\n", champName, err)
	} else {
		fmt.Printf("Fetched %d counters for %s:\n", len(counters), champName)
		for _, m := range counters {
			enemyName := a.champions.GetName(m.EnemyChampionID)
			iconURL := a.champions.GetIconURL(m.EnemyChampionID)
			fmt.Printf("  - %s: %.1f%% WR (%d games)\n", enemyName, m.WinRate, m.Matches)
			result.Counters = append(result.Counters, ChampionDetailMatchup{
				ChampionID:   m.EnemyChampionID,
				ChampionName: enemyName,
				IconURL:      iconURL,
				WinRate:      m.WinRate,
				Games:        m.Matches,
			})
		}
		if len(counters) > 0 {
			result.HasData = true
		}
	}

	// Fetch good matchups (champions you beat)
	allMatchups, err := a.statsProvider.FetchAllMatchups(championID, role)
	if err == nil && len(allMatchups) > 0 {
		result.HasData = true

		// Sort allMatchups by win rate descending
		for i := 0; i < len(allMatchups); i++ {
			for j := i + 1; j < len(allMatchups); j++ {
				if allMatchups[j].WinRate > allMatchups[i].WinRate {
					allMatchups[i], allMatchups[j] = allMatchups[j], allMatchups[i]
				}
			}
		}
		for i := 0; i < min(5, len(allMatchups)); i++ {
			m := allMatchups[i]
			if m.Matches < 20 {
				continue
			}
			enemyName := a.champions.GetName(m.EnemyChampionID)
			iconURL := a.champions.GetIconURL(m.EnemyChampionID)
			result.GoodMatchups = append(result.GoodMatchups, ChampionDetailMatchup{
				ChampionID:   m.EnemyChampionID,
				ChampionName: enemyName,
				IconURL:      iconURL,
				WinRate:      m.WinRate,
				Games:        m.Matches,
			})
		}
	}

	return result
}
