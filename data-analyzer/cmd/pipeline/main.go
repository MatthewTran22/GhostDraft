package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"data-analyzer/internal/collector"
	"data-analyzer/internal/db"
	"data-analyzer/internal/discord"
	"data-analyzer/internal/riot"
	"data-analyzer/internal/storage"

	"github.com/joho/godotenv"
)

func main() {
	// Flags
	riotID := flag.String("riot-id", "", "Starting Riot ID (e.g., 'Player#NA1')")
	matchCount := flag.Int("count", 20, "Number of matches to fetch per player")
	maxPlayers := flag.Int("max-players", 100, "Maximum unique players to collect")
	outputDir := flag.String("output-dir", "./export", "Directory for reducer output")
	skipCollector := flag.Bool("reduce-only", false, "Skip collector, only run reducer")
	continuous := flag.Bool("continuous", false, "Run in continuous mode (24/7 collection)")
	flag.Parse()

	// Load .env
	envPaths := []string{".env", "../.env", "../../.env", "data-analyzer/.env"}
	for _, path := range envPaths {
		if err := godotenv.Load(path); err == nil {
			fmt.Printf("Loaded .env from: %s\n", path)
			break
		}
	}

	// Continuous mode - run the ContinuousCollector
	if *continuous {
		runContinuousMode()
		return
	}

	// Find the data-analyzer directory
	analyzerDir := findAnalyzerDir()
	if analyzerDir == "" {
		log.Fatal("Could not find data-analyzer directory")
	}
	fmt.Printf("Working directory: %s\n", analyzerDir)

	startTime := time.Now()

	// Step 1: Run collector (unless skip flag set)
	if !*skipCollector {
		fmt.Println("\n========================================")
		fmt.Println("STEP 1: COLLECTING MATCH DATA")
		fmt.Println("========================================")

		collectorArgs := []string{
			"run", "./cmd/collector",
			fmt.Sprintf("--count=%d", *matchCount),
			fmt.Sprintf("--max-players=%d", *maxPlayers),
		}

		// Only add --riot-id if explicitly provided (otherwise collector auto-seeds from Challenger)
		if *riotID != "" {
			collectorArgs = append(collectorArgs, "--riot-id="+*riotID)
		} else {
			fmt.Println("No --riot-id provided, collector will auto-seed from Challenger leaderboard")
		}

		if err := runCommand(analyzerDir, "go", collectorArgs...); err != nil {
			log.Fatalf("Collector failed: %v", err)
		}

		fmt.Printf("\nCollection completed in %s\n", time.Since(startTime).Round(time.Second))
	}

	// Step 2: Run reducer
	fmt.Println("\n========================================")
	fmt.Println("STEP 2: REDUCING & EXPORTING DATA")
	fmt.Println("========================================")

	reducerArgs := []string{
		"run", "./cmd/reducer",
		"--output-dir=" + *outputDir,
	}

	if err := runCommand(analyzerDir, "go", reducerArgs...); err != nil {
		log.Fatalf("Reducer failed: %v", err)
	}

	totalTime := time.Since(startTime).Round(time.Second)

	fmt.Println("\n========================================")
	fmt.Println("PIPELINE COMPLETE")
	fmt.Println("========================================")
	fmt.Printf("Total time: %s\n", totalTime)
	fmt.Printf("Output: %s/data.json\n", *outputDir)
	fmt.Println("\nNext steps:")
	fmt.Println("  1. Upload data.json to your CDN/GitHub")
	fmt.Println("  2. Update manifest.json with new version")
	fmt.Println("  3. Restart the app or call ForceStatsUpdate()")
}

// runContinuousMode runs the continuous collector (24/7 mode)
func runContinuousMode() {
	log.Println("========================================")
	log.Println("CONTINUOUS COLLECTION MODE")
	log.Println("========================================")
	log.Println("Press Ctrl+C to initiate graceful shutdown")
	log.Println("")

	// Validate required environment variables
	if os.Getenv("RIOT_API_KEY") == "" {
		log.Fatal("RIOT_API_KEY environment variable is required")
	}

	// Get storage path
	storagePath := os.Getenv("BLOB_STORAGE_PATH")
	if storagePath == "" {
		storagePath = "./data"
	}

	// Get Discord bot token and channel (used for both notifications AND key retrieval)
	discordBotToken := os.Getenv("DISCORD_BOT_TOKEN")
	discordChannelID := os.Getenv("DISCORD_CHANNEL_ID")
	if discordBotToken != "" && discordChannelID != "" {
		log.Println("Discord bot: enabled (notifications + key retrieval)")
	} else {
		log.Println("Discord bot: disabled (set DISCORD_BOT_TOKEN and DISCORD_CHANNEL_ID to enable)")
	}

	// Get Turso credentials
	tursoURL := os.Getenv("TURSO_DATABASE_URL")
	tursoToken := os.Getenv("TURSO_AUTH_TOKEN")
	var tursoClient *db.TursoClient
	var tursoPusher *collector.TursoPusher
	if tursoURL != "" {
		var err error
		tursoClient, err = db.NewTursoClient(tursoURL, tursoToken)
		if err != nil {
			log.Printf("Warning: Failed to connect to Turso: %v (pushes will be skipped)", err)
		} else {
			log.Println("Turso: connected")
			defer tursoClient.Close()

			// Create TursoPusher with adapter
			dataPusher := collector.NewTursoDataPusher(tursoClient)
			tursoPusher = collector.NewTursoPusher(dataPusher)
		}
	} else {
		log.Println("Turso: disabled (set TURSO_DATABASE_URL to enable)")
	}

	// Create Riot API client
	riotClient, err := riot.NewClient()
	if err != nil {
		log.Fatalf("Failed to create Riot client: %v", err)
	}

	// Get current patch
	ctx := context.Background()
	currentPatch, err := riot.GetCurrentPatch(ctx)
	if err != nil {
		log.Fatalf("Failed to get current patch: %v", err)
	}
	log.Printf("Current patch: %s", currentPatch)

	// Create file rotator
	rotator, err := storage.NewFileRotator(storagePath)
	if err != nil {
		log.Fatalf("Failed to create file rotator: %v", err)
	}
	defer rotator.Close()

	// Create the real Spider with continuous mode config
	// Read config from environment variables (with defaults)
	matchesPerPlayer := getEnvInt("MATCHES_PER_PLAYER", 20)
	maxPlayers := getEnvInt("MAX_PLAYERS", 10000)
	workerCount := getEnvInt("WORKER_COUNT", 1)
	timelineSamplingRate := getEnvFloat("TIMELINE_SAMPLING_RATE", 0.20)

	log.Printf("Config: matches_per_player=%d, max_players=%d, workers=%d, timeline_rate=%.2f",
		matchesPerPlayer, maxPlayers, workerCount, timelineSamplingRate)

	spiderConfig := collector.SpiderConfig{
		MatchesPerPlayer:     matchesPerPlayer,
		MaxPlayers:           maxPlayers,
		WorkerCount:          workerCount,
		TimelineSamplingRate: timelineSamplingRate,
	}
	spider := collector.NewSpider(riotClient, rotator, currentPatch, spiderConfig)

	// Create API key validator
	keyValidator := riot.NewKeyValidator()

	// Create Discord bot client (handles both notifications and key retrieval)
	var discordBot *discord.KeyFinder
	var keyProvider collector.KeyProvider
	if discordBotToken != "" && discordChannelID != "" {
		discordBot = discord.NewKeyFinder(discordBotToken, discordChannelID)
		keyProvider = &keyFinderAdapter{finder: discordBot}
	}

	// Validate API key at startup
	log.Println("Validating API key...")
	apiKey := os.Getenv("RIOT_API_KEY")
	valid, validationErr := keyValidator.ValidateKey(ctx, apiKey)

	// Only request new key if we DEFINITIVELY know it's invalid
	// If validation errored (timeout, network issue), continue and let spider handle it
	keyDefinitelyInvalid := !valid && validationErr == nil
	if validationErr != nil {
		log.Printf("Warning: Could not validate API key: %v (will attempt to continue)", validationErr)
	}
	if keyDefinitelyInvalid {
		log.Println("API key is invalid or expired!")
	}

	if keyDefinitelyInvalid {
		// Send Discord notification for key issues
		if discordBot != nil {
			log.Println("Sending Discord notification about key issue...")
			payload := discord.NewKeyExpiredPayload(0, 0, 0)
			if err := discordBot.SendEmbed(ctx, payload); err != nil {
				log.Printf("Failed to send Discord notification: %v", err)
			} else {
				log.Println("Discord notification sent!")
			}
		}

		// Wait for new key if provider is configured
		if keyProvider != nil {
			log.Println("Waiting for new API key from Discord...")
			for {
				newKey, err := keyProvider.WaitForKey(ctx)
				if err != nil {
					log.Printf("Error waiting for key: %v", err)
					time.Sleep(5 * time.Minute)
					continue
				}

				// Validate new key
				newValid, newErr := keyValidator.ValidateKey(ctx, newKey)
				if newErr != nil {
					log.Printf("Error validating new key: %v", newErr)
					continue
				}
				if !newValid {
					log.Println("Received invalid key, continuing to wait...")
					continue
				}

				// Valid key received - update environment and recreate client
				log.Println("Valid key received! Restarting with new key...")
				os.Setenv("RIOT_API_KEY", newKey)

				// Send success notification
				if discordBot != nil {
					payload := discord.NewSessionStartedPayload(newKey, "Challenger #1")
					discordBot.SendEmbed(ctx, payload)
				}

				// Recreate Riot client with new key
				riotClient, err = riot.NewClient()
				if err != nil {
					log.Fatalf("Failed to create Riot client with new key: %v", err)
				}

				// Recreate spider with new client
				spider = collector.NewSpider(riotClient, rotator, currentPatch, spiderConfig)
				break
			}
		} else if !valid {
			// No key provider configured - wait indefinitely to prevent restart spam
			// User must set DISCORD_BOT_TOKEN and DISCORD_CHANNEL_ID, then restart
			log.Println("ERROR: API key is invalid and no key provider configured.")
			log.Println("To enable automatic key renewal, set these environment variables:")
			log.Println("  - DISCORD_BOT_TOKEN: Your Discord bot token")
			log.Println("  - DISCORD_CHANNEL_ID: Channel ID where you'll post new keys")
			log.Println("")
			log.Println("Waiting indefinitely to prevent notification spam...")
			log.Println("Update your .env file and restart the container when ready.")

			// Block forever to prevent container restart spam
			select {}
		}
		// If validation errored but we don't have key provider, continue and let the collector handle it
	} else {
		log.Println("API key is valid!")
	}

	// We'll set up the notifyFunc after creating cc so it can access stats
	var cc *collector.ContinuousCollector

	// Create notify function with access to collector stats
	notifyFunc := func(notifyCtx context.Context, message string) error {
		log.Printf("[Notify] %s", message)
		if discordBot == nil {
			return nil
		}

		// Determine notification type and send appropriate embed
		if strings.Contains(message, "expired") {
			// Get actual stats from the collector
			var matchesCollected int
			var runtime, lastReduceAgo time.Duration
			if cc != nil {
				stats := cc.GetStats()
				matchesCollected = int(stats.MatchesCollected)
				runtime = time.Duration(stats.RuntimeSeconds) * time.Second
				if stats.LastReduceAgo >= 0 {
					lastReduceAgo = time.Duration(stats.LastReduceAgo) * time.Second
				}
			}
			payload := discord.NewKeyExpiredPayload(matchesCollected, runtime, lastReduceAgo)
			return discordBot.SendEmbed(notifyCtx, payload)
		} else if strings.Contains(message, "started") {
			payload := discord.NewSessionStartedPayload("validated", "Challenger #1")
			return discordBot.SendEmbed(notifyCtx, payload)
		}

		// Generic message - log only
		return nil
	}

	// Create reduce function using real components
	warmDir := filepath.Join(storagePath, "warm")
	coldDir := filepath.Join(storagePath, "cold")
	reduceFunc := func(reduceCtx context.Context) error {
		log.Println("[Reduce] ========================================")
		log.Println("[Reduce] Starting reduce cycle...")
		log.Printf("[Reduce] Warm directory: %s", warmDir)
		log.Printf("[Reduce] Cold directory: %s", coldDir)

		// List warm directory contents before processing
		warmFiles, _ := filepath.Glob(filepath.Join(warmDir, "*.jsonl"))
		log.Printf("[Reduce] Found %d warm files to process", len(warmFiles))
		for i, f := range warmFiles {
			if info, err := os.Stat(f); err == nil {
				log.Printf("[Reduce]   [%d] %s (%.2f MB)", i+1, filepath.Base(f), float64(info.Size())/(1024*1024))
			}
		}

		// First, flush hot file to warm
		log.Println("[Reduce] Flushing hot file to warm...")
		if rotated, err := rotator.FlushAndRotate(); err != nil {
			log.Printf("[Reduce] Warning: FlushAndRotate failed: %v", err)
		} else if rotated {
			log.Println("[Reduce] Flushed hot file to warm")
			// Re-check warm files after flush
			warmFiles, _ = filepath.Glob(filepath.Join(warmDir, "*.jsonl"))
			log.Printf("[Reduce] After flush: %d warm files", len(warmFiles))
		} else {
			log.Println("[Reduce] No hot file to flush (or empty)")
		}

		// Aggregate warm files
		log.Println("[Reduce] Aggregating warm files...")
		agg, err := collector.AggregateWarmFiles(warmDir, riot.IsCompletedItem)
		if err != nil {
			log.Printf("[Reduce] ERROR: Aggregation failed: %v", err)
			return fmt.Errorf("aggregation failed: %w", err)
		}

		log.Printf("[Reduce] Aggregated %d files, %d records, patch %s",
			agg.FilesProcessed, agg.TotalRecords, agg.DetectedPatch)
		log.Printf("[Reduce] Stats: %d champion stats, %d item stats, %d item slot stats, %d matchup stats",
			len(agg.ChampionStats), len(agg.ItemStats), len(agg.ItemSlotStats), len(agg.MatchupStats))

		// Archive warm files to cold
		archived, err := collector.ArchiveWarmToCold(warmDir, coldDir)
		if err != nil {
			return fmt.Errorf("archiving failed: %w", err)
		}
		log.Printf("[Reduce] Archived %d files to cold storage", archived)

		// Push to Turso asynchronously if available
		if tursoPusher != nil && agg.TotalRecords > 0 {
			log.Println("[Reduce] Queueing Turso push...")
			if err := tursoPusher.Push(reduceCtx, agg); err != nil {
				log.Printf("[Reduce] Warning: Failed to queue Turso push: %v", err)
			} else {
				log.Println("[Reduce] Turso push queued (running in background)")
			}
		} else if tursoPusher == nil {
			log.Println("[Reduce] Turso push: skipped (no Turso connection)")
		} else {
			log.Println("[Reduce] Turso push: skipped (no records)")
		}

		log.Println("[Reduce] Reduce cycle complete")
		log.Println("[Reduce] ========================================")
		return nil
	}

	// Create configuration
	config := collector.DefaultConfig()
	warmFileThreshold := getEnvInt("WARM_FILE_THRESHOLD", 10)
	config.WarmFileThreshold = int64(warmFileThreshold)
	log.Printf("Reduce trigger: every %d warm files", warmFileThreshold)

	// Create continuous collector
	cc = collector.NewContinuousCollector(
		spider,
		reduceFunc,
		&keyValidatorAdapter{keyValidator},
		keyProvider,
		notifyFunc,
		config,
	)

	// Wire up rotator callback to increment warm file count
	rotator.SetOnRotateCallback(func() {
		log.Println("[Rotator] File rotated to warm, incrementing counter...")
		cc.IncrementWarmFileCount()
	})

	// Start TursoPusher background worker if available
	if tursoPusher != nil {
		tursoPusher.Start(ctx)
		defer func() {
			log.Println("Waiting for pending Turso pushes to complete...")
			tursoPusher.Wait()
			log.Println("All Turso pushes complete")
		}()
	}

	// Set up signal handler for graceful shutdown
	signalCtx := collector.SetupSignalHandler(func(shutdownCtx context.Context) {
		log.Println("Initiating graceful shutdown...")
		cc.Shutdown(shutdownCtx)
	})

	// Run the continuous collector
	log.Println("Starting continuous collector...")
	if err := cc.Run(signalCtx); err != nil && err != context.Canceled {
		log.Printf("Continuous collector error: %v", err)
	}

	log.Println("Continuous collector stopped")
}

// keyFinderAdapter adapts discord.KeyFinder to collector.KeyProvider interface
type keyFinderAdapter struct {
	finder *discord.KeyFinder
	since  time.Time
}

func (a *keyFinderAdapter) WaitForKey(ctx context.Context) (string, error) {
	// On first call, set since to now to avoid picking up old messages
	if a.since.IsZero() {
		a.since = time.Now()
	}
	return a.finder.WaitForKey(ctx, a.since)
}

// keyValidatorAdapter adapts riot.KeyValidator to collector.KeyValidator interface
type keyValidatorAdapter struct {
	validator *riot.KeyValidator
}

func (a *keyValidatorAdapter) ValidateKey(key string) (bool, error) {
	return a.validator.ValidateKey(context.Background(), key)
}

func runCommand(dir, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	fmt.Printf("Running: %s %s\n\n", name, strings.Join(args, " "))
	return cmd.Run()
}

func findAnalyzerDir() string {
	// Try common locations
	candidates := []string{
		".",
		"data-analyzer",
		"../data-analyzer",
		"../../data-analyzer",
	}

	for _, candidate := range candidates {
		path := filepath.Join(candidate, "cmd", "collector", "main.go")
		if _, err := os.Stat(path); err == nil {
			abs, _ := filepath.Abs(candidate)
			return abs
		}
	}

	return ""
}

// getEnvInt reads an integer from an environment variable, returning defaultVal if not set or invalid
func getEnvInt(key string, defaultVal int) int {
	val := os.Getenv(key)
	if val == "" {
		return defaultVal
	}
	var result int
	if _, err := fmt.Sscanf(val, "%d", &result); err != nil {
		log.Printf("Warning: Invalid value for %s=%q, using default %d", key, val, defaultVal)
		return defaultVal
	}
	return result
}

// getEnvFloat reads a float from an environment variable, returning defaultVal if not set or invalid
func getEnvFloat(key string, defaultVal float64) float64 {
	val := os.Getenv(key)
	if val == "" {
		return defaultVal
	}
	var result float64
	if _, err := fmt.Sscanf(val, "%f", &result); err != nil {
		log.Printf("Warning: Invalid value for %s=%q, using default %.2f", key, val, defaultVal)
		return defaultVal
	}
	return result
}
