package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"ghostdraft/internal/data"
	"ghostdraft/internal/lcu"
	"ghostdraft/internal/stats"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App struct
type App struct {
	ctx              context.Context
	lcuClient        *lcu.Client
	wsClient         *lcu.WebSocketClient
	champions        *lcu.ChampionRegistry
	items            *lcu.ItemRegistry
	championDB       *data.ChampionDB
	statsDB          *data.StatsDB    // SQLite database for stats
	statsProvider    *stats.Provider  // Stats queries (uses statsDB)
	stopPoll         chan struct{}
	lastFetchedChamp int
	lastFetchedEnemy int
	lastBanFetchKey  string
	lastItemFetchKey string
	windowVisible    bool
}

// NewApp creates a new App application struct
func NewApp() *App {
	return &App{
		lcuClient:     lcu.NewClient(),
		wsClient:      lcu.NewWebSocketClient(),
		champions:     lcu.NewChampionRegistry(),
		items:         lcu.NewItemRegistry(),
		stopPoll:      make(chan struct{}),
		windowVisible: true,
	}
}

// startup is called when the app starts
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

	// Initialize champion database
	if db, err := data.NewChampionDB(); err != nil {
		fmt.Printf("Failed to initialize champion DB: %v\n", err)
	} else {
		a.championDB = db
		fmt.Println("Champion database initialized")
	}

	// Position and size window relative to screen
	screens, err := runtime.ScreenGetAll(ctx)
	if err == nil && len(screens) > 0 {
		screen := screens[0]
		// Size: ~20% width, ~55% height (roughly matches champ select sidebar)
		width := screen.Size.Width * 20 / 100
		height := screen.Size.Height * 55 / 100
		runtime.WindowSetSize(ctx, width, height)
		// Position at right edge, vertically centered
		x := screen.Size.Width - width - 20
		y := (screen.Size.Height - height) / 2
		runtime.WindowSetPosition(ctx, x, y)
	}

	// Load data from Data Dragon in parallel
	go func() {
		if err := a.champions.Load(); err != nil {
			fmt.Printf("Failed to load champions: %v\n", err)
		}
	}()
	go func() {
		if err := a.items.Load(); err != nil {
			fmt.Printf("Failed to load items: %v\n", err)
		}
	}()

	// Initialize stats database and check for updates
	go func() {
		// Create local SQLite database for stats
		statsDB, err := data.NewStatsDB()
		if err != nil {
			fmt.Printf("Failed to open stats database: %v\n", err)
			return
		}
		a.statsDB = statsDB

		// Check for updates from remote manifest
		manifestURL := os.Getenv("STATS_MANIFEST_URL")
		if manifestURL == "" {
			manifestURL = data.DefaultManifestURL
		}

		if err := statsDB.CheckForUpdates(manifestURL); err != nil {
			fmt.Printf("Failed to check for stats updates: %v (using cached data)\n", err)
		}

		if !statsDB.HasData() {
			fmt.Println("No stats data available")
			return
		}

		// Create stats provider
		provider, err := stats.NewProvider(statsDB)
		if err != nil {
			fmt.Printf("Stats provider not available: %v\n", err)
			return
		}

		// If current patch not set from update, fetch from database
		if provider.GetPatch() == "" {
			if err := provider.FetchPatch(); err != nil {
				fmt.Printf("No stats patch available: %v\n", err)
				return
			}
		}

		a.statsProvider = provider
		fmt.Printf("Stats provider ready (patch %s)\n", provider.GetPatch())
	}()

	// Set up champ select handler
	a.wsClient.SetChampSelectHandler(a.onChampSelectUpdate)

	// Start polling for League Client
	go a.pollForLeagueClient()

	// Register global hotkey (Ctrl+O to toggle visibility)
	a.RegisterToggleHotkey()
}

// shutdown is called when the app is closing
func (a *App) shutdown(ctx context.Context) {
	close(a.stopPoll)
	a.wsClient.Disconnect()
	a.lcuClient.Disconnect()
	if a.championDB != nil {
		a.championDB.Close()
	}
	if a.statsDB != nil {
		a.statsDB.Close()
	}
}

// onChampSelectUpdate handles champ select state changes
func (a *App) onChampSelectUpdate(session *lcu.ChampSelectSession, inChampSelect bool) {
	if !inChampSelect {
		a.lastFetchedChamp = 0
		a.lastFetchedEnemy = 0
		a.lastBanFetchKey = ""
		a.lastItemFetchKey = ""
		runtime.EventsEmit(a.ctx, "champselect:update", map[string]interface{}{
			"inChampSelect": false,
		})
		runtime.EventsEmit(a.ctx, "build:update", map[string]interface{}{
			"hasBuild": false,
		})
		runtime.EventsEmit(a.ctx, "bans:update", map[string]interface{}{
			"hasBans": false,
		})
		runtime.EventsEmit(a.ctx, "items:update", map[string]interface{}{
			"hasItems": false,
		})
		fmt.Println("Exited Champion Select")
		return
	}

	// Find local player's champion and position
	var localChampionID int
	var localPosition string
	foundPlayer := false
	for _, player := range session.MyTeam {
		if player.CellID == session.LocalPlayerCellID {
			localChampionID = player.ChampionID
			localPosition = player.GetPosition()
			foundPlayer = true
			break
		}
	}

	// If position is empty, try to infer from other players' positions
	if foundPlayer && localPosition == "" {
		allPositions := map[string]bool{
			"top": false, "jungle": false, "middle": false, "bottom": false, "utility": false,
		}
		for _, player := range session.MyTeam {
			pos := player.GetPosition()
			if pos != "" {
				allPositions[pos] = true
			}
		}
		// Find the missing position
		for pos, taken := range allPositions {
			if !taken {
				localPosition = pos
				fmt.Printf("Inferred position: %s (missing from team)\n", localPosition)
				break
			}
		}
	}

	fmt.Printf("Player CellID: %d, Position: '%s', ChampID: %d\n",
		session.LocalPlayerCellID, localPosition, localChampionID)

	// Collect all enemy champion IDs
	var enemyChampionIDs []int
	fmt.Printf("Enemy team size: %d\n", len(session.TheirTeam))
	for _, enemy := range session.TheirTeam {
		if enemy.ChampionID > 0 {
			enemyChampionIDs = append(enemyChampionIDs, enemy.ChampionID)
			fmt.Printf("  Enemy: ChampID=%d\n", enemy.ChampionID)
		}
	}

	// Find current action (hover/pick) - check for any action with a selected champion
	var currentAction *lcu.ChampSelectAction
	for _, actionGroup := range session.Actions {
		for i := range actionGroup {
			action := &actionGroup[i]
			if action.ActorCellID == session.LocalPlayerCellID && !action.Completed && action.ChampionID > 0 {
				currentAction = action
				break
			}
		}
		if currentAction != nil {
			break
		}
	}

	// Determine phase and hovered champion
	var hoveredChampionID int
	var actionType string
	if currentAction != nil {
		hoveredChampionID = currentAction.ChampionID
		actionType = currentAction.Type
		fmt.Printf("Current action: Type=%s, ChampionID=%d, IsInProgress=%v, Completed=%v\n",
			currentAction.Type, currentAction.ChampionID, currentAction.IsInProgress, currentAction.Completed)
	} else {
		fmt.Println("No current action (not your turn)")
	}

	// Get champion names
	var championName string
	var championID int
	var isLocked bool
	if hoveredChampionID > 0 {
		championName = a.champions.GetName(hoveredChampionID)
		championID = hoveredChampionID
	} else if localChampionID > 0 {
		championName = a.champions.GetName(localChampionID)
		championID = localChampionID
		isLocked = true
	}

	fmt.Printf("Final: championID=%d, championName=%s, lastFetched=%d\n", championID, championName, a.lastFetchedChamp)

	data := map[string]interface{}{
		"inChampSelect": true,
		"phase":         session.Timer.Phase,
		"championName":  championName,
		"championID":    championID,
		"isLocked":      isLocked,
		"localPosition": localPosition,
		"actionType":    actionType,
		"timeLeft":      session.Timer.TimeLeftInPhase,
	}

	runtime.EventsEmit(a.ctx, "champselect:update", data)

	// Check if all bans are completed (we're in pick phase)
	hasIncompleteBan := false
	for _, actionGroup := range session.Actions {
		for _, action := range actionGroup {
			if action.Type == "ban" && !action.Completed {
				hasIncompleteBan = true
				break
			}
		}
		if hasIncompleteBan {
			break
		}
	}

	// Show recommended bans whenever we have a champion + role
	fmt.Printf("Ban check: championID=%d, localPosition='%s', lastBanFetchKey='%s'\n", championID, localPosition, a.lastBanFetchKey)
	if championID > 0 && localPosition != "" {
		banKey := fmt.Sprintf("%d-%s", championID, localPosition)
		if banKey != a.lastBanFetchKey {
			fmt.Printf("Triggering ban fetch for key: %s\n", banKey)
			a.lastBanFetchKey = banKey
			go a.fetchAndEmitRecommendedBans(championID, localPosition)
		} else {
			fmt.Printf("Skipping ban fetch - same key: %s\n", banKey)
		}

		// Also fetch item build when champion + role changes
		itemKey := fmt.Sprintf("%d-%s", championID, localPosition)
		if itemKey != a.lastItemFetchKey {
			a.lastItemFetchKey = itemKey
			go a.fetchAndEmitItems(championID, championName, localPosition)
		}
	}

	// Analyze team composition for damage balance
	a.analyzeTeamComp(session, localChampionID)

	// Analyze full team comps when all locked
	a.analyzeFullComp(session)

	// During ban phase, don't fetch matchup data yet
	if hasIncompleteBan {
		return
	}

	// Fetch build data when champion changes or new enemies appear
	if championID > 0 && championID != a.lastFetchedChamp {
		a.lastFetchedChamp = championID
		go a.fetchAndEmitBuild(championID, championName, localPosition, enemyChampionIDs)
	} else if len(enemyChampionIDs) > 0 && len(enemyChampionIDs) != a.lastFetchedEnemy {
		a.lastFetchedEnemy = len(enemyChampionIDs)
		go a.fetchAndEmitBuild(championID, championName, localPosition, enemyChampionIDs)
	}
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

// analyzeTeamComp checks team damage balance and emits recommendation
func (a *App) analyzeTeamComp(session *lcu.ChampSelectSession, localChampID int) {
	if a.championDB == nil {
		fmt.Println("Team comp: championDB is nil!")
		return
	}
	fmt.Println("Analyzing team comp...")

	var apCount, adCount, mixedCount int
	localHasLocked := false

	for _, player := range session.MyTeam {
		// Skip players without a champion
		if player.ChampionID == 0 {
			continue
		}

		// Skip local player - we want to advise them, not count their hover
		if player.CellID == session.LocalPlayerCellID {
			// Check if local player has locked
			for _, actionGroup := range session.Actions {
				for _, action := range actionGroup {
					if action.ActorCellID == player.CellID && action.Type == "pick" && action.Completed {
						localHasLocked = true
						break
					}
				}
				if localHasLocked {
					break
				}
			}
			continue
		}

		// Count teammate's champion damage type
		champName := a.champions.GetName(player.ChampionID)
		dmgType := a.championDB.GetDamageType(champName)

		fmt.Printf("  Teammate %s: %s\n", champName, dmgType)

		switch dmgType {
		case "AP":
			apCount++
		case "AD":
			adCount++
		default:
			// Mixed types like AD/AP, AP/Tank count as 0.5 each
			if strings.Contains(dmgType, "AP") {
				apCount++
			}
			if strings.Contains(dmgType, "AD") {
				adCount++
			}
			if dmgType == "Tank" {
				mixedCount++
			}
		}
	}

	totalDmgChamps := apCount + adCount
	fmt.Printf("Team comp analysis: AP=%d, AD=%d, Mixed=%d, LocalLocked=%v\n", apCount, adCount, mixedCount, localHasLocked)

	// Don't show recommendation if local player already locked
	if localHasLocked {
		runtime.EventsEmit(a.ctx, "teamcomp:update", map[string]interface{}{
			"show": false,
		})
		return
	}

	// Need at least 1 teammate to assess balance
	if totalDmgChamps < 1 {
		runtime.EventsEmit(a.ctx, "teamcomp:update", map[string]interface{}{
			"show": false,
		})
		return
	}

	var recommendation string
	var severity string // "warning" or "critical"

	apRatio := float64(apCount) / float64(totalDmgChamps)
	adRatio := float64(adCount) / float64(totalDmgChamps)

	if apRatio >= 0.75 {
		recommendation = "Team is AP heavy - consider picking AD"
		if apRatio >= 0.9 {
			severity = "critical"
		} else {
			severity = "warning"
		}
	} else if adRatio >= 0.75 {
		recommendation = "Team is AD heavy - consider picking AP"
		if adRatio >= 0.9 {
			severity = "critical"
		} else {
			severity = "warning"
		}
	}

	if recommendation != "" {
		fmt.Printf("Team comp: AP=%d, AD=%d, Mixed=%d - %s\n", apCount, adCount, mixedCount, recommendation)
		runtime.EventsEmit(a.ctx, "teamcomp:update", map[string]interface{}{
			"show":           true,
			"recommendation": recommendation,
			"severity":       severity,
			"apCount":        apCount,
			"adCount":        adCount,
		})
	} else {
		runtime.EventsEmit(a.ctx, "teamcomp:update", map[string]interface{}{
			"show": false,
		})
	}
}

// TeamCompData holds analyzed team composition data
type TeamCompData struct {
	Tags       map[string]int
	AP         int
	AD         int
	Archetype  string
	HasTank    bool
	HasPick    bool // Single-target CC (hooks, roots)
}

// analyzeFullComp analyzes both teams when all players have locked in
func (a *App) analyzeFullComp(session *lcu.ChampSelectSession) {
	if a.championDB == nil {
		return
	}

	// Check if all players have locked in
	allLocked := true
	for _, player := range session.MyTeam {
		if player.ChampionID == 0 {
			allLocked = false
			break
		}
	}
	for _, player := range session.TheirTeam {
		if player.ChampionID == 0 {
			allLocked = false
			break
		}
	}

	if !allLocked {
		runtime.EventsEmit(a.ctx, "fullcomp:update", map[string]interface{}{
			"ready": false,
		})
		return
	}

	// Analyze both teams
	allyComp := a.analyzeTeamTags(session.MyTeam)
	enemyComp := a.analyzeTeamTags(session.TheirTeam)

	// Calculate damage percentages
	allyTotal := allyComp.AP + allyComp.AD
	enemyTotal := enemyComp.AP + enemyComp.AD
	allyAPPct, allyADPct := 50, 50
	enemyAPPct, enemyADPct := 50, 50

	if allyTotal > 0 {
		allyAPPct = allyComp.AP * 100 / allyTotal
		allyADPct = 100 - allyAPPct
	}
	if enemyTotal > 0 {
		enemyAPPct = enemyComp.AP * 100 / enemyTotal
		enemyADPct = 100 - enemyAPPct
	}

	fmt.Printf("Full comp: Ally=%s (AP=%d%% AD=%d%%), Enemy=%s (AP=%d%% AD=%d%%)\n",
		allyComp.Archetype, allyAPPct, allyADPct, enemyComp.Archetype, enemyAPPct, enemyADPct)

	runtime.EventsEmit(a.ctx, "fullcomp:update", map[string]interface{}{
		"ready":         true,
		"allyArchetype": allyComp.Archetype,
		"allyTags":      formatTagCounts(allyComp.Tags),
		"allyAP":        allyAPPct,
		"allyAD":        allyADPct,
		"enemyArchetype": enemyComp.Archetype,
		"enemyTags":      formatTagCounts(enemyComp.Tags),
		"enemyAP":        enemyAPPct,
		"enemyAD":        enemyADPct,
	})
}

// analyzeTeamTags analyzes a team's composition
func (a *App) analyzeTeamTags(team []lcu.ChampSelectPlayer) TeamCompData {
	comp := TeamCompData{
		Tags: make(map[string]int),
	}

	for _, player := range team {
		if player.ChampionID == 0 {
			continue
		}

		champName := a.champions.GetName(player.ChampionID)
		info, _ := a.championDB.GetChampion(champName)
		if info == nil {
			continue
		}

		// Count damage type
		if strings.Contains(info.DamageType, "AP") {
			comp.AP++
		}
		if strings.Contains(info.DamageType, "AD") {
			comp.AD++
		}
		if info.DamageType == "Tank" {
			comp.HasTank = true
		}

		// Count role tags
		tags := info.RoleTags
		for _, tag := range strings.Split(tags, ", ") {
			tag = strings.TrimSpace(tag)
			// Normalize tags - extract base tag
			baseTag := tag
			if strings.Contains(tag, "(") {
				baseTag = strings.TrimSpace(tag[:strings.Index(tag, "(")])
			}
			if baseTag != "" {
				comp.Tags[baseTag]++
			}

			// Check for pick potential (single-target CC)
			if strings.Contains(tag, "Pick") {
				comp.HasPick = true
			}
			if baseTag == "Tank" {
				comp.HasTank = true
			}
		}
	}

	// Determine archetype
	comp.Archetype = determineArchetype(comp)

	return comp
}

// determineArchetype determines the team's primary archetype
func determineArchetype(comp TeamCompData) string {
	engageCount := comp.Tags["Engage"]
	pokeCount := comp.Tags["Poke"]
	burstCount := comp.Tags["Burst"]
	tankCount := comp.Tags["Tank"]
	bruiserCount := comp.Tags["Bruiser"]
	disengageCount := comp.Tags["Disengage"]

	// Hard Engage: 3+ Engage, usually has Tank/Bruiser
	if engageCount >= 3 && (comp.HasTank || bruiserCount >= 1) {
		return "Hard Engage"
	}

	// Poke/Siege: 3+ Poke, lacks hard engage or has disengage
	if pokeCount >= 3 && (engageCount < 2 || disengageCount >= 1) {
		return "Poke/Siege"
	}

	// Pick Comp: 3+ Burst with single-target CC
	if burstCount >= 3 && comp.HasPick {
		return "Pick Comp"
	}

	// Teamfight: Good balance of engage + burst
	if engageCount >= 2 && burstCount >= 2 {
		return "Teamfight"
	}

	// Skirmish/Split: Bruiser heavy, less teamfight
	if bruiserCount >= 3 {
		return "Skirmish"
	}

	// Tank heavy
	if tankCount >= 2 {
		return "Front-to-Back"
	}

	// Default based on highest count
	if pokeCount >= 2 {
		return "Poke"
	}
	if engageCount >= 2 {
		return "Engage"
	}
	if burstCount >= 2 {
		return "Burst"
	}

	return "Mixed"
}

// formatTagCounts formats tags for display
func formatTagCounts(tags map[string]int) []string {
	var result []string
	// Priority order for display
	priority := []string{"Engage", "Burst", "Poke", "Tank", "Bruiser", "Disengage"}

	for _, tag := range priority {
		if count, ok := tags[tag]; ok && count >= 2 {
			result = append(result, fmt.Sprintf("%s (%d)", tag, count))
		}
	}
	return result
}

// pollForLeagueClient continuously checks for League Client
func (a *App) pollForLeagueClient() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	wasConnected := false

	// Try immediately on startup
	a.tryConnect()
	if a.lcuClient.IsConnected() {
		wasConnected = true
		a.connectWebSocket()
	}

	for {
		select {
		case <-a.stopPoll:
			return
		case <-ticker.C:
			isConnected := a.lcuClient.IsConnected()

			if isConnected && !wasConnected {
				// Just connected
				wasConnected = true
				a.connectWebSocket()
			} else if !isConnected {
				// If we were connected before, emit disconnect event
				if wasConnected {
					a.wsClient.Disconnect()
					runtime.EventsEmit(a.ctx, "lcu:status", map[string]interface{}{
						"connected": false,
						"message":   "League Disconnected. Waiting...",
					})
					runtime.EventsEmit(a.ctx, "champselect:update", map[string]interface{}{
						"inChampSelect": false,
					})
					runtime.EventsEmit(a.ctx, "build:update", map[string]interface{}{
						"hasBuild": false,
					})
					fmt.Println("League Disconnected. Waiting for reconnection...")
					wasConnected = false
				}
				// Try to reconnect
				a.tryConnect()
				if a.lcuClient.IsConnected() {
					wasConnected = true
					a.connectWebSocket()
				}
			} else if isConnected && !a.wsClient.IsConnected() {
				// HTTP connected but WebSocket disconnected, try to reconnect WS
				a.connectWebSocket()
			}
		}
	}
}

// connectWebSocket establishes WebSocket connection
func (a *App) connectWebSocket() {
	creds := a.lcuClient.GetCredentials()
	if creds == nil {
		return
	}

	err := a.wsClient.Connect(creds)
	if err != nil {
		fmt.Printf("WebSocket connection failed: %v\n", err)
		return
	}

	fmt.Println("WebSocket connected - Listening for champ select...")
}

// tryConnect attempts to connect to the League Client
func (a *App) tryConnect() {
	err := a.lcuClient.Connect()
	if err != nil {
		runtime.EventsEmit(a.ctx, "lcu:status", map[string]interface{}{
			"connected": false,
			"message":   "Waiting for League...",
		})
		return
	}

	// Successfully connected
	runtime.EventsEmit(a.ctx, "lcu:status", map[string]interface{}{
		"connected": true,
		"message":   "League Connected!",
		"port":      a.lcuClient.GetPort(),
	})

	fmt.Printf("League Connected! Port: %s\n", a.lcuClient.GetPort())
}

// GetConnectionStatus returns the current LCU connection status
func (a *App) GetConnectionStatus() map[string]interface{} {
	if a.lcuClient.IsConnected() {
		return map[string]interface{}{
			"connected": true,
			"message":   "League Connected!",
			"port":      a.lcuClient.GetPort(),
		}
	}
	return map[string]interface{}{
		"connected": false,
		"message":   "Waiting for League...",
	}
}

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
	Patch   string                   `json:"patch"`
	HasData bool                     `json:"hasData"`
	Roles   map[string][]MetaChampion `json:"roles"`
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
