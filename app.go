package main

import (
	"context"
	"fmt"
	"os"

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
	statsDB          *data.StatsDB   // SQLite database for stats
	statsProvider    *stats.Provider // Stats queries (uses statsDB)
	stopPoll         chan struct{}
	lastFetchedChamp    int
	lastFetchedEnemy    int
	lastBanFetchKey     string
	lastItemFetchKey    string
	lastCounterFetchKey string
	windowVisible       bool
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
	go a.initStats()

	// Set up champ select handler
	a.wsClient.SetChampSelectHandler(a.onChampSelectUpdate)

	// Start polling for League Client
	go a.pollForLeagueClient()

	// Register global hotkey (Ctrl+O to toggle visibility)
	a.RegisterToggleHotkey()
}

// initStats initializes the stats database and provider
func (a *App) initStats() {
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
