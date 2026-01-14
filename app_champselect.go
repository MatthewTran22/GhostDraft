package main

import (
	"fmt"

	"ghostdraft/internal/data"
	"ghostdraft/internal/lcu"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// onChampSelectUpdate handles champ select state changes
func (a *App) onChampSelectUpdate(session *lcu.ChampSelectSession, inChampSelect bool) {
	if !inChampSelect {
		a.lastFetchedChamp = 0
		a.lastFetchedEnemy = 0
		a.lastBanFetchKey = ""
		a.lastItemFetchKey = ""
		a.lastCounterFetchKey = ""
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
		runtime.EventsEmit(a.ctx, "counterpicks:update", map[string]interface{}{
			"hasData": false,
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
		actionType = currentAction.Type
		// Only track hovered champion for pick actions, not ban actions
		if currentAction.Type == "pick" {
			hoveredChampionID = currentAction.ChampionID
		}
		fmt.Printf("Current action: Type=%s, ChampionID=%d, IsInProgress=%v, Completed=%v\n",
			currentAction.Type, currentAction.ChampionID, currentAction.IsInProgress, currentAction.Completed)
	} else {
		fmt.Println("No current action (not your turn)")
	}

	// Get champion names - only use pick actions or locked champion, not ban hovers
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

	// Save locked champion data for in-game use
	if isLocked && localPosition != "" {
		a.lockedChampionID = championID
		a.lockedChampionName = championName
		a.lockedPosition = localPosition
		fmt.Printf("Saved locked champion: %s (%d) %s\n", championName, championID, localPosition)
	}

	fmt.Printf("Final: championID=%d, championName=%s, lastFetched=%d\n", championID, championName, a.lastFetchedChamp)

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

	data := map[string]interface{}{
		"inChampSelect":   true,
		"phase":           session.Timer.Phase,
		"championName":    championName,
		"championID":      championID,
		"isLocked":        isLocked,
		"localPosition":   localPosition,
		"actionType":      actionType,
		"timeLeft":        session.Timer.TimeLeftInPhase,
		"banPhaseComplete": !hasIncompleteBan,
	}

	runtime.EventsEmit(a.ctx, "champselect:update", data)

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

	// Find enemy laner (same position as us)
	var enemyLanerID int
	for _, enemy := range session.TheirTeam {
		if enemy.ChampionID > 0 && enemy.GetPosition() == localPosition {
			enemyLanerID = enemy.ChampionID
			break
		}
	}

	// Fetch counter picks for enemy laner (after ban phase)
	if enemyLanerID > 0 && localPosition != "" {
		counterKey := fmt.Sprintf("counter-%d-%s", enemyLanerID, localPosition)
		if counterKey != a.lastCounterFetchKey {
			a.lastCounterFetchKey = counterKey
			go a.fetchAndEmitCounterPicks(enemyLanerID, localPosition)
		}
	} else {
		// No enemy laner visible yet
		runtime.EventsEmit(a.ctx, "counterpicks:update", map[string]interface{}{
			"hasData": false,
		})
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

// onGameflowUpdate handles gameflow phase changes
func (a *App) onGameflowUpdate(phase string) {
	fmt.Printf("Gameflow update: %s\n", phase)
	runtime.EventsEmit(a.ctx, "gameflow:update", map[string]interface{}{
		"phase": phase,
	})

	// When entering a game, hide overlay and fetch data
	if phase == "InProgress" {
		a.HideForGame()
		go a.fetchAndEmitInGameBuild()
		go a.fetchAndEmitScouting()
	} else if phase == "None" || phase == "Lobby" || phase == "Matchmaking" {
		// When leaving a game, show overlay again and clear locked data
		a.ShowAfterGame()
		a.lockedChampionID = 0
		a.lockedChampionName = ""
		a.lockedPosition = ""
	}
}

// fetchAndEmitInGameBuild fetches the build for the current in-game champion
func (a *App) fetchAndEmitInGameBuild() {
	var championID int
	var championName string
	var role string

	// Try to use saved champ select data first
	if a.lockedChampionID > 0 && a.lockedPosition != "" {
		championID = a.lockedChampionID
		championName = a.lockedChampionName
		role = a.lockedPosition
		fmt.Printf("Using saved champ select data: %s (%d) %s\n", championName, championID, role)
	} else if a.currentPUUID != "" {
		// Fallback: Use stored PUUID to find ourselves in game
		fmt.Println("No champ select data, using PUUID to find player in game...")

		session, err := a.lcuClient.GetGameSession()
		if err != nil {
			runtime.EventsEmit(a.ctx, "ingame:build", map[string]interface{}{
				"hasBuild": false,
				"error":    "Failed to get game session",
			})
			return
		}

		// Find ourselves in the game
		found := false
		for _, player := range session.GameData.TeamOne {
			if player.PUUID == a.currentPUUID {
				championID = player.ChampionID
				found = true
				break
			}
		}
		if !found {
			for _, player := range session.GameData.TeamTwo {
				if player.PUUID == a.currentPUUID {
					championID = player.ChampionID
					found = true
					break
				}
			}
		}

		if !found || championID == 0 {
			runtime.EventsEmit(a.ctx, "ingame:build", map[string]interface{}{
				"hasBuild": false,
				"error":    "Could not find player in game",
			})
			return
		}

		championName = a.champions.GetName(championID)

		// Get most played role for this champion from stats
		if a.statsProvider != nil {
			role = a.statsProvider.GetMostPlayedRole(championID)
		}
		if role == "" {
			role = "middle" // Default fallback
		}

		fmt.Printf("Found via PUUID: %s (%d), inferred role: %s\n", championName, championID, role)
	} else {
		runtime.EventsEmit(a.ctx, "ingame:build", map[string]interface{}{
			"hasBuild": false,
			"error":    "No player data available",
		})
		return
	}

	// Fetch build data from stats provider
	if a.statsProvider == nil {
		runtime.EventsEmit(a.ctx, "ingame:build", map[string]interface{}{
			"hasBuild":     false,
			"championName": championName,
			"championID":   championID,
			"error":        "Stats not available",
		})
		return
	}

	// Fetch item build using existing method
	buildData, err := a.statsProvider.FetchChampionData(championID, championName, role)
	if err != nil {
		// Try without role filter
		buildData, err = a.statsProvider.FetchChampionData(championID, championName, "")
	}

	if err != nil || len(buildData.Builds) == 0 {
		runtime.EventsEmit(a.ctx, "ingame:build", map[string]interface{}{
			"hasBuild":     false,
			"championName": championName,
			"championID":   championID,
			"role":         role,
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
	convertItemOptions := func(options []data.ItemOption) []map[string]interface{} {
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

	// Convert builds to frontend format
	var builds []map[string]interface{}
	for _, build := range buildData.Builds {
		builds = append(builds, map[string]interface{}{
			"coreItems":   convertItems(build.CoreItems),
			"fourthItems": convertItemOptions(build.FourthItemOptions),
			"fifthItems":  convertItemOptions(build.FifthItemOptions),
			"sixthItems":  convertItemOptions(build.SixthItemOptions),
		})
	}

	fmt.Printf("Emitting in-game build for %s: %d build paths\n", championName, len(builds))

	runtime.EventsEmit(a.ctx, "ingame:build", map[string]interface{}{
		"hasBuild":     true,
		"championName": championName,
		"championID":   championID,
		"championIcon": a.champions.GetIconURL(championID),
		"role":         role,
		"builds":       builds,
	})
}

// normalizePosition converts LCU position strings to our format
func normalizePosition(position string) string {
	switch position {
	case "TOP":
		return "top"
	case "JUNGLE":
		return "jungle"
	case "MIDDLE", "MID":
		return "middle"
	case "BOTTOM", "ADC":
		return "bottom"
	case "UTILITY", "SUPPORT":
		return "utility"
	default:
		return ""
	}
}

// PlayerStats represents calculated stats for a player
type PlayerStats struct {
	PUUID        string
	GameName     string
	TagLine      string
	ChampionID   int
	ChampionName string
	ChampionIcon string
	Team         int
	IsMe         bool
	// Stats from recent games
	Games        int
	Wins         int
	WinRate      float64
	AvgKills     float64
	AvgDeaths    float64
	AvgAssists   float64
	KDA          float64
	AvgCS        float64
	// Tilt detection
	RecentLosses int     // losses in last 3 games
	WorstGame    string  // e.g., "0/8/2 on Yasuo"
	TiltLevel    string  // "tilted", "warming_up", "on_fire", ""
	FunFact      string  // funny observation
}

// fetchAndEmitScouting fetches all players' recent stats
func (a *App) fetchAndEmitScouting() {
	fmt.Println("Fetching scouting data...")

	players, myPUUID, err := a.lcuClient.GetGamePlayers()
	if err != nil {
		fmt.Printf("Failed to get game players: %v\n", err)
		runtime.EventsEmit(a.ctx, "ingame:scouting", map[string]interface{}{
			"hasData": false,
			"error":   err.Error(),
		})
		return
	}

	fmt.Printf("Found %d players in game\n", len(players))

	var allStats []PlayerStats

	for _, player := range players {
		stats := a.calculatePlayerStats(player, myPUUID)
		allStats = append(allStats, stats)
	}

	// Separate into teams
	var myTeam, enemyTeam []map[string]interface{}
	var myTeamNum int

	for _, s := range allStats {
		if s.IsMe {
			myTeamNum = s.Team
		}
	}

	for _, s := range allStats {
		playerData := map[string]interface{}{
			"puuid":        s.PUUID,
			"gameName":     s.GameName,
			"tagLine":      s.TagLine,
			"championId":   s.ChampionID,
			"championName": s.ChampionName,
			"championIcon": s.ChampionIcon,
			"isMe":         s.IsMe,
			"games":        s.Games,
			"wins":         s.Wins,
			"winRate":      s.WinRate,
			"avgKills":     s.AvgKills,
			"avgDeaths":    s.AvgDeaths,
			"avgAssists":   s.AvgAssists,
			"kda":          s.KDA,
			"avgCS":        s.AvgCS,
			"tiltLevel":    s.TiltLevel,
			"funFact":      s.FunFact,
		}

		if s.Team == myTeamNum {
			myTeam = append(myTeam, playerData)
		} else {
			enemyTeam = append(enemyTeam, playerData)
		}
	}

	fmt.Printf("Scouting complete: %d allies, %d enemies\n", len(myTeam), len(enemyTeam))

	runtime.EventsEmit(a.ctx, "ingame:scouting", map[string]interface{}{
		"hasData":   true,
		"myTeam":    myTeam,
		"enemyTeam": enemyTeam,
	})
}

// calculatePlayerStats calculates stats for a single player
func (a *App) calculatePlayerStats(player lcu.GamePlayer, myPUUID string) PlayerStats {
	stats := PlayerStats{
		PUUID:        player.PUUID,
		ChampionID:   player.ChampionID,
		ChampionName: a.champions.GetName(player.ChampionID),
		ChampionIcon: a.champions.GetIconURL(player.ChampionID),
		Team:         player.Team,
		IsMe:         player.PUUID == myPUUID,
	}

	// Fetch match history
	history, err := a.lcuClient.GetMatchHistoryByPUUID(player.PUUID, 20)
	if err != nil {
		fmt.Printf("Failed to get match history for %s: %v\n", player.PUUID[:8], err)
		stats.FunFact = "Mystery player - no history found"
		return stats
	}

	matches := history.Games.Games
	if len(matches) == 0 {
		stats.FunFact = "Fresh account or first games"
		return stats
	}

	// Calculate stats from ranked/normal games only
	var totalKills, totalDeaths, totalAssists, totalCS int
	var recentResults []bool // true = win, last 3 games
	var worstKDA float64 = 999
	var worstGameStr string

	for _, match := range matches {
		// Skip custom games, ARAM, etc. - focus on SR
		if match.GameMode != "CLASSIC" {
			continue
		}

		// The first participant is the player we're looking at
		if len(match.Participants) == 0 {
			continue
		}

		p := match.Participants[0]
		s := p.Stats

		stats.Games++
		if s.Win {
			stats.Wins++
		}

		totalKills += s.Kills
		totalDeaths += s.Deaths
		totalAssists += s.Assists
		totalCS += s.TotalMinionsKilled + s.NeutralMinionsKilled

		// Track recent results (up to 3)
		if len(recentResults) < 3 {
			recentResults = append(recentResults, s.Win)
		}

		// Track worst game
		deaths := s.Deaths
		if deaths == 0 {
			deaths = 1
		}
		gameKDA := float64(s.Kills+s.Assists) / float64(deaths)
		if gameKDA < worstKDA && s.Deaths >= 5 {
			worstKDA = gameKDA
			champName := a.champions.GetName(p.ChampionId)
			worstGameStr = fmt.Sprintf("%d/%d/%d on %s", s.Kills, s.Deaths, s.Assists, champName)
		}
	}

	if stats.Games > 0 {
		stats.WinRate = float64(stats.Wins) / float64(stats.Games) * 100
		stats.AvgKills = float64(totalKills) / float64(stats.Games)
		stats.AvgDeaths = float64(totalDeaths) / float64(stats.Games)
		stats.AvgAssists = float64(totalAssists) / float64(stats.Games)
		stats.AvgCS = float64(totalCS) / float64(stats.Games)

		avgDeaths := stats.AvgDeaths
		if avgDeaths == 0 {
			avgDeaths = 1
		}
		stats.KDA = (stats.AvgKills + stats.AvgAssists) / avgDeaths
	}

	// Count recent losses
	for _, won := range recentResults {
		if !won {
			stats.RecentLosses++
		}
	}

	// Determine tilt level and fun facts
	stats.TiltLevel, stats.FunFact = a.detectTiltAndFunFact(stats, worstGameStr, recentResults)

	return stats
}

// GetGoldDiff fetches live gold data based on items - exposed to frontend
func (a *App) GetGoldDiff() map[string]interface{} {
	players, err := a.liveClient.GetAllPlayers()
	if err != nil {
		return map[string]interface{}{
			"hasData": false,
			"error":   "Game not running or live client unavailable",
		}
	}

	activePlayerName, _ := a.liveClient.GetActivePlayer()

	// Group players by team and calculate gold
	var orderPlayers, chaosPlayers []map[string]interface{}
	var myTeam string

	for _, player := range players {
		// Calculate item gold
		var itemGold int
		var itemList []map[string]interface{}
		for _, item := range player.Items {
			gold := a.items.GetGold(item.ItemID)
			itemGold += gold
			if item.ItemID > 0 {
				itemList = append(itemList, map[string]interface{}{
					"id":      item.ItemID,
					"name":    item.DisplayName,
					"gold":    gold,
					"iconURL": a.items.GetIconURL(item.ItemID),
				})
			}
		}

		isMe := player.SummonerName == activePlayerName
		if isMe {
			myTeam = player.Team
		}

		playerData := map[string]interface{}{
			"summonerName": player.SummonerName,
			"championName": player.ChampionName,
			"championIcon": a.champions.GetIconURLByName(player.RawChampionName),
			"position":     player.Position,
			"team":         player.Team,
			"isMe":         isMe,
			"level":        player.Level,
			"kills":        player.Scores.Kills,
			"deaths":       player.Scores.Deaths,
			"assists":      player.Scores.Assists,
			"cs":           player.Scores.CreepScore,
			"itemGold":     itemGold,
			"items":        itemList,
		}

		if player.Team == "ORDER" {
			orderPlayers = append(orderPlayers, playerData)
		} else {
			chaosPlayers = append(chaosPlayers, playerData)
		}
	}

	// Calculate team totals
	var orderGold, chaosGold int
	for _, p := range orderPlayers {
		orderGold += p["itemGold"].(int)
	}
	for _, p := range chaosPlayers {
		chaosGold += p["itemGold"].(int)
	}

	// Determine which team is "my team" vs "enemy team"
	var myTeamPlayers, enemyTeamPlayers []map[string]interface{}
	var myTeamGold, enemyTeamGold int
	if myTeam == "ORDER" {
		myTeamPlayers = orderPlayers
		enemyTeamPlayers = chaosPlayers
		myTeamGold = orderGold
		enemyTeamGold = chaosGold
	} else {
		myTeamPlayers = chaosPlayers
		enemyTeamPlayers = orderPlayers
		myTeamGold = chaosGold
		enemyTeamGold = orderGold
	}

	// Calculate matchup diffs by position
	matchups := a.calculatePositionMatchups(myTeamPlayers, enemyTeamPlayers)

	return map[string]interface{}{
		"hasData":        true,
		"myTeam":         myTeamPlayers,
		"enemyTeam":      enemyTeamPlayers,
		"myTeamGold":     myTeamGold,
		"enemyTeamGold":  enemyTeamGold,
		"goldDiff":       myTeamGold - enemyTeamGold,
		"matchups":       matchups,
	}
}

// calculatePositionMatchups matches players by position and calculates gold diff
func (a *App) calculatePositionMatchups(myTeam, enemyTeam []map[string]interface{}) []map[string]interface{} {
	var matchups []map[string]interface{}

	positionOrder := []string{"TOP", "JUNGLE", "MIDDLE", "BOTTOM", "UTILITY"}

	for _, pos := range positionOrder {
		var myPlayer, enemyPlayer map[string]interface{}

		for _, p := range myTeam {
			if p["position"] == pos {
				myPlayer = p
				break
			}
		}
		for _, p := range enemyTeam {
			if p["position"] == pos {
				enemyPlayer = p
				break
			}
		}

		if myPlayer != nil && enemyPlayer != nil {
			myGold := myPlayer["itemGold"].(int)
			enemyGold := enemyPlayer["itemGold"].(int)
			diff := myGold - enemyGold

			matchups = append(matchups, map[string]interface{}{
				"position":     pos,
				"myPlayer":     myPlayer,
				"enemyPlayer":  enemyPlayer,
				"goldDiff":     diff,
			})
		}
	}

	return matchups
}

// detectTiltAndFunFact generates tilt status and fun observations
func (a *App) detectTiltAndFunFact(stats PlayerStats, worstGame string, recentResults []bool) (string, string) {
	var tiltLevel, funFact string

	// Check recent streak
	allLosses := len(recentResults) >= 3 && !recentResults[0] && !recentResults[1] && !recentResults[2]
	allWins := len(recentResults) >= 3 && recentResults[0] && recentResults[1] && recentResults[2]

	if allLosses {
		tiltLevel = "tilted"
		funFact = "Lost 3 in a row - probably tilted"
	} else if allWins {
		tiltLevel = "on_fire"
		funFact = "On a 3 game win streak!"
	} else if stats.RecentLosses >= 2 {
		tiltLevel = "warming_up"
		funFact = "Rough start today"
	}

	// Override with specific observations
	if stats.WinRate < 40 && stats.Games >= 5 {
		funFact = fmt.Sprintf("%.0f%% WR in %d games... yikes", stats.WinRate, stats.Games)
		tiltLevel = "tilted"
	} else if stats.WinRate > 60 && stats.Games >= 5 {
		funFact = fmt.Sprintf("%.0f%% WR smurf alert!", stats.WinRate)
		tiltLevel = "on_fire"
	}

	if stats.AvgDeaths > 7 {
		funFact = fmt.Sprintf("Dies %.1f times per game on average", stats.AvgDeaths)
	}

	if stats.KDA > 4 && stats.Games >= 3 {
		funFact = fmt.Sprintf("%.1f KDA - this one's dangerous", stats.KDA)
		tiltLevel = "on_fire"
	} else if stats.KDA < 1.5 && stats.Games >= 3 {
		funFact = fmt.Sprintf("%.1f KDA - free gold", stats.KDA)
	}

	if worstGame != "" && stats.AvgDeaths > 5 {
		funFact = fmt.Sprintf("Recent int game: %s", worstGame)
	}

	if stats.Games == 0 {
		funFact = "No recent ranked games"
	}

	return tiltLevel, funFact
}
