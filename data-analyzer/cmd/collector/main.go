package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"data-analyzer/internal/riot"
	"data-analyzer/internal/storage"

	"github.com/bits-and-blooms/bloom/v3"
	"github.com/joho/godotenv"
)

func main() {
	// Load .env file - try multiple locations
	envPaths := []string{".env", "../.env", "../../.env", "data-analyzer/.env"}
	envLoaded := false
	for _, path := range envPaths {
		if err := godotenv.Load(path); err == nil {
			fmt.Printf("Loaded .env from: %s\n", path)
			envLoaded = true
			break
		}
	}
	if !envLoaded {
		log.Println("No .env file found, using environment variables")
	}

	// Parse flags
	riotID := flag.String("riot-id", "", "Starting Riot ID (e.g., 'Player#NA1')")
	puuid := flag.String("puuid", "", "Starting PUUID")
	matchCount := flag.Int("count", 20, "Number of matches to fetch per player")
	maxPlayers := flag.Int("max-players", 100, "Maximum unique players to collect")
	flag.Parse()

	// Get blob storage path from env (required)
	dataDir := os.Getenv("BLOB_STORAGE_PATH")
	if dataDir == "" {
		log.Fatal("BLOB_STORAGE_PATH environment variable not set")
	}
	// Remove quotes if present (from .env parsing)
	dataDir = strings.Trim(dataDir, "\"")
	fmt.Printf("Using storage path: %s\n", dataDir)

	if *riotID == "" && *puuid == "" {
		fmt.Println("Usage:")
		fmt.Println("  collector --riot-id='Player#NA1' [--count=20] [--max-players=100]")
		fmt.Println("  collector --puuid=PUUID [--count=20] [--max-players=100]")
		fmt.Println()
		fmt.Println("Storage path is set via BLOB_STORAGE_PATH in .env")
		fmt.Println()
		fmt.Println("This will collect matches from the starting player, then snowball")
		fmt.Println("to collect matches from other players found in those matches.")
		fmt.Println()
		fmt.Println("Data is written to rotating JSONL files in:")
		fmt.Println("  hot/   - Active writes")
		fmt.Println("  warm/  - Closed files awaiting processing")
		fmt.Println("  cold/  - Compressed archives")
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		fmt.Println("\n[Shutdown] Gracefully shutting down...")
		cancel()
	}()

	// Create file rotator
	rotator, err := storage.NewFileRotator(dataDir)
	if err != nil {
		log.Fatalf("Failed to create file rotator: %v", err)
	}
	defer func() {
		if err := rotator.Close(); err != nil {
			log.Printf("Error closing rotator: %v", err)
		}
	}()

	// Create Riot API client
	client, err := riot.NewClient()
	if err != nil {
		log.Fatalf("Failed to create Riot client: %v", err)
	}

	// Fetch current patch from Data Dragon
	currentPatch, err := riot.GetCurrentPatch(ctx)
	if err != nil {
		log.Fatalf("Failed to get current patch: %v", err)
	}
	fmt.Printf("Current patch: %s (only collecting matches from this patch)\n", currentPatch)

	// Get starting PUUID
	var startingPUUID string
	if *riotID != "" {
		parts := strings.SplitN(*riotID, "#", 2)
		if len(parts) != 2 {
			log.Fatalf("Invalid Riot ID format '%s', expected 'GameName#TagLine'", *riotID)
		}

		gameName := url.PathEscape(strings.TrimSpace(parts[0]))
		tagLine := url.PathEscape(strings.TrimSpace(parts[1]))

		fmt.Printf("Looking up Riot ID: %s#%s...\n", parts[0], parts[1])
		account, err := client.GetAccountByRiotID(ctx, gameName, tagLine)
		if err != nil {
			log.Fatalf("Failed to lookup %s: %v", *riotID, err)
		}
		fmt.Printf("  Found PUUID: %s\n", account.PUUID)
		startingPUUID = account.PUUID
	} else {
		startingPUUID = *puuid
	}

	// Bloom filters for deduplication (space-efficient for large datasets)
	// Sized for 500k matches and 1M players with 0.1% false positive rate
	visitedMatches := bloom.NewWithEstimates(500000, 0.001)
	visitedPUUIDs := bloom.NewWithEstimates(1000000, 0.001)

	// Queue of PUUIDs to process
	queue := []string{startingPUUID}
	visitedPUUIDs.AddString(startingPUUID)

	playerCount := 0
	totalMatchesWritten := 0
	startTime := time.Now()

	// Process queue
	for len(queue) > 0 && playerCount < *maxPlayers {
		// Check for cancellation
		select {
		case <-ctx.Done():
			fmt.Println("[Shutdown] Stopping collection...")
			goto shutdown
		default:
		}

		// Pop from queue
		currentPUUID := queue[0]
		queue = queue[1:]

		elapsed := time.Since(startTime)

		fmt.Printf("\n[Player %d/%d] [%s elapsed] Processing: %s\n",
			playerCount, *maxPlayers, formatDuration(elapsed), currentPUUID[:20]+"...")

		// Fetch match history
		matchIDs, err := client.GetMatchHistory(ctx, currentPUUID, *matchCount)
		if err != nil {
			log.Printf("  Failed to fetch match history: %v", err)
			continue
		}
		fmt.Printf("  Found %d matches\n", len(matchIDs))

		// Process each match
		matchesThisPlayer := 0
		currentPatchMatches := 0
		newPlayersFromThisUser := []string{} // Buffer new players, only add if mostly current patch

		for _, matchID := range matchIDs {
			// Check for cancellation
			select {
			case <-ctx.Done():
				fmt.Println("[Shutdown] Stopping collection...")
				goto shutdown
			default:
			}

			// Bloom filter deduplication check
			if visitedMatches.TestString(matchID) {
				continue
			}
			visitedMatches.AddString(matchID)

			// Fetch match details
			match, err := client.GetMatch(ctx, matchID)
			if err != nil {
				log.Printf("    Failed to fetch match: %v", err)
				continue
			}

			// Check if match is from current patch
			// Matches are ordered most recent first, so if we hit an old patch,
			// all remaining matches will also be old - break early to save API calls
			matchPatch := riot.NormalizePatch(match.Info.GameVersion)
			if matchPatch != currentPatch {
				break
			}
			currentPatchMatches++

			// Write each participant as a separate record
			// NOTE: Timeline fetch removed for 2x speed improvement
			// Using final inventory (item0-5) instead of build order from timeline
			for _, participant := range match.Info.Participants {
				rawMatch := &storage.RawMatch{
					MatchID:      matchID,
					GameVersion:  match.Info.GameVersion,
					GameDuration: match.Info.GameDuration,
					GameCreation: match.Info.GameCreation,
					PUUID:        participant.PUUID,
					GameName:     participant.RiotIdGameName,
					TagLine:      participant.RiotIdTagline,
					ChampionID:   participant.ChampionID,
					ChampionName: participant.ChampionName,
					TeamPosition: participant.TeamPosition,
					Win:          participant.Win,
					Item0:        participant.Item0,
					Item1:        participant.Item1,
					Item2:        participant.Item2,
					Item3:        participant.Item3,
					Item4:        participant.Item4,
					Item5:        participant.Item5,
				}

				if err := rotator.WriteLine(rawMatch); err != nil {
					log.Printf("    Failed to write record: %v", err)
					continue
				}

				// Buffer new players (will add to queue only if user has mostly current patch matches)
				if !visitedPUUIDs.TestString(participant.PUUID) {
					newPlayersFromThisUser = append(newPlayersFromThisUser, participant.PUUID)
				}
			}

			// Signal match complete (increments counter, may trigger rotation)
			if err := rotator.MatchComplete(); err != nil {
				log.Printf("    Failed to complete match: %v", err)
			}

			matchesThisPlayer++
			totalMatchesWritten++
		}

		// Always add co-players from current patch games (they might be active)
		for _, puuid := range newPlayersFromThisUser {
			if !visitedPUUIDs.TestString(puuid) {
				visitedPUUIDs.AddString(puuid)
				queue = append(queue, puuid)
			}
		}

		// Only count this player towards max if they had mostly current patch matches
		totalMatches := len(matchIDs)
		isActive := totalMatches > 0 && currentPatchMatches*2 >= totalMatches
		if isActive {
			playerCount++
		}

		// Summary log for this player
		status := "ACTIVE"
		if !isActive {
			status = "STALE"
		}
		fmt.Printf("  [%s] %d matches collected, %d total, queue: %d\n",
			status, matchesThisPlayer, totalMatchesWritten, len(queue))
	}

shutdown:
	// Print summary
	totalDuration := time.Since(startTime)
	fmt.Printf("\n=== Collection Complete ===\n")
	fmt.Printf("Total time: %s\n", formatDuration(totalDuration))
	fmt.Printf("Players processed: %d\n", playerCount)
	fmt.Printf("Matches written: %d\n", totalMatchesWritten)
	fmt.Printf("Total records (participants): %d\n", totalMatchesWritten*10)
	fmt.Printf("Remaining queue: %d\n", len(queue))
	if totalMatchesWritten > 0 {
		avgPerMatch := totalDuration / time.Duration(totalMatchesWritten)
		fmt.Printf("Avg time per match: %s\n", formatDuration(avgPerMatch))
	}
}

// formatDuration formats a duration in a human-readable way
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	} else if d < time.Hour {
		mins := int(d.Minutes())
		secs := int(d.Seconds()) % 60
		return fmt.Sprintf("%dm%02ds", mins, secs)
	}
	hours := int(d.Hours())
	mins := int(d.Minutes()) % 60
	secs := int(d.Seconds()) % 60
	return fmt.Sprintf("%dh%02dm%02ds", hours, mins, secs)
}
