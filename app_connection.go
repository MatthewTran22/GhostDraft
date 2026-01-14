package main

import (
	"fmt"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

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

	// Fetch initial gameflow state
	go a.fetchInitialGameflow()
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

	// Store current user's PUUID for in-game identification
	if puuid, err := a.lcuClient.GetCurrentSummonerPUUID(); err == nil {
		a.currentPUUID = puuid
		fmt.Printf("Stored user PUUID: %s\n", puuid[:8])
	}

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

// fetchInitialGameflow fetches the current gameflow phase on connect
func (a *App) fetchInitialGameflow() {
	phase, err := a.lcuClient.GetGameflowPhase()
	if err != nil {
		fmt.Printf("Failed to get initial gameflow phase: %v\n", err)
		return
	}

	fmt.Printf("Initial gameflow phase: %s\n", phase)
	a.onGameflowUpdate(phase)
}

// GetGameflowPhase returns the current gameflow phase (exposed to frontend for debugging)
func (a *App) GetGameflowPhase() map[string]interface{} {
	if !a.lcuClient.IsConnected() {
		return map[string]interface{}{
			"error": "Not connected",
		}
	}

	phase, err := a.lcuClient.GetGameflowPhase()
	if err != nil {
		return map[string]interface{}{
			"error": err.Error(),
		}
	}

	// If in game, also trigger build and scouting fetch
	if phase == "InProgress" {
		go a.fetchAndEmitInGameBuild()
		go a.fetchAndEmitScouting()
	}

	return map[string]interface{}{
		"phase": phase,
	}
}
