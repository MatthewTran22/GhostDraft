package main

import (
	"context"
	"fmt"
	"time"

	"ghostdraft/internal/lcu"
	"ghostdraft/internal/ugg"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App struct
type App struct {
	ctx              context.Context
	lcuClient        *lcu.Client
	wsClient         *lcu.WebSocketClient
	champions        *lcu.ChampionRegistry
	uggFetcher       *ugg.Fetcher
	stopPoll         chan struct{}
	lastFetchedChamp int
	lastFetchedEnemy int
}

// NewApp creates a new App application struct
func NewApp() *App {
	return &App{
		lcuClient:  lcu.NewClient(),
		wsClient:   lcu.NewWebSocketClient(),
		champions:  lcu.NewChampionRegistry(),
		uggFetcher: ugg.NewFetcher(),
		stopPoll:   make(chan struct{}),
	}
}

// startup is called when the app starts
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

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

	// Load data from Data Dragon and U.GG in parallel
	go func() {
		if err := a.champions.Load(); err != nil {
			fmt.Printf("Failed to load champions: %v\n", err)
		}
	}()
	go func() {
		if err := a.uggFetcher.FetchPatch(); err != nil {
			fmt.Printf("Failed to fetch U.GG patch: %v\n", err)
		}
	}()

	// Set up champ select handler
	a.wsClient.SetChampSelectHandler(a.onChampSelectUpdate)

	// Start polling for League Client
	go a.pollForLeagueClient()
}

// shutdown is called when the app is closing
func (a *App) shutdown(ctx context.Context) {
	close(a.stopPoll)
	a.wsClient.Disconnect()
	a.lcuClient.Disconnect()
}

// onChampSelectUpdate handles champ select state changes
func (a *App) onChampSelectUpdate(session *lcu.ChampSelectSession, inChampSelect bool) {
	if !inChampSelect {
		a.lastFetchedChamp = 0
		a.lastFetchedEnemy = 0
		runtime.EventsEmit(a.ctx, "champselect:update", map[string]interface{}{
			"inChampSelect": false,
		})
		runtime.EventsEmit(a.ctx, "build:update", map[string]interface{}{
			"hasBuild": false,
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

	// Only fetch matchup data after all bans are done
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

// fetchAndEmitBuild fetches matchup data from U.GG and emits it to frontend
func (a *App) fetchAndEmitBuild(championID int, championName string, role string, enemyChampionIDs []int) {
	fmt.Printf("Fetching U.GG matchup for %s (%s) vs %d enemies...\n", championName, role, len(enemyChampionIDs))

	if len(enemyChampionIDs) == 0 {
		runtime.EventsEmit(a.ctx, "build:update", map[string]interface{}{
			"hasBuild":     true,
			"championName": championName,
			"role":         role,
			"winRate":      "-",
			"winRateLabel": "Waiting for enemy...",
			"patch":        a.uggFetcher.GetPatch(),
		})
		fmt.Printf("No enemies detected yet for %s\n", championName)
		return
	}

	// Fetch our matchups once - this gives us all enemies we face in our role
	matchups, err := a.uggFetcher.FetchMatchups(championID, role)
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
	var matchupGames float64
	for _, enemyID := range enemyChampionIDs {
		for _, m := range matchups {
			if m.EnemyChampionID == enemyID && m.Games > matchupGames {
				laneOpponentID = enemyID
				matchupWR = m.WinRate
				matchupGames = m.Games
			}
		}
	}
	if laneOpponentID > 0 {
		fmt.Printf("Lane opponent (highest games): %d (%.1f%% WR, %.0f games)\n", laneOpponentID, matchupWR, matchupGames)
	}

	if laneOpponentID == 0 {
		runtime.EventsEmit(a.ctx, "build:update", map[string]interface{}{
			"hasBuild":     true,
			"championName": championName,
			"role":         role,
			"winRate":      "-",
			"winRateLabel": "No lane opponent found",
			"patch":        a.uggFetcher.GetPatch(),
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

	fmt.Printf("Matchup: %s vs %s = %.1f%% (%s, %.0f games)\n", championName, enemyName, matchupWR, matchupStatus, matchupGames)
	runtime.EventsEmit(a.ctx, "build:update", map[string]interface{}{
		"hasBuild":      true,
		"championName":  championName,
		"role":          role,
		"winRate":       fmt.Sprintf("%.1f%%", matchupWR),
		"winRateLabel":  fmt.Sprintf("vs %s", enemyName),
		"enemyName":     enemyName,
		"matchupStatus": matchupStatus,
		"patch":         a.uggFetcher.GetPatch(),
	})
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
