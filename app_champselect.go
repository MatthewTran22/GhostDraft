package main

import (
	"fmt"

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
