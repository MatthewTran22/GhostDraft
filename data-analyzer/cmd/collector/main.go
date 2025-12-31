package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/url"
	"os"
	"strings"

	"data-analyzer/internal/db"
	"data-analyzer/internal/riot"

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

	if *riotID == "" && *puuid == "" {
		fmt.Println("Usage:")
		fmt.Println("  collector --riot-id='Player#NA1' [--count=20] [--max-players=20]")
		fmt.Println("  collector --puuid=PUUID [--count=20] [--max-players=20]")
		fmt.Println()
		fmt.Println("This will collect matches from the starting player, then snowball")
		fmt.Println("to collect matches from other players found in those matches.")
		os.Exit(1)
	}

	ctx := context.Background()

	// Connect to database
	database, err := db.New(ctx)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer database.Close()

	// Create Riot API client
	client, err := riot.NewClient()
	if err != nil {
		log.Fatalf("Failed to create Riot client: %v", err)
	}

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

	// Queue of PUUIDs to process
	queue := []string{startingPUUID}
	processed := make(map[string]bool)
	discovered := make(map[string]bool)
	discovered[startingPUUID] = true

	playerCount := 0

	// Process queue
	for len(queue) > 0 && playerCount < *maxPlayers {
		// Pop from queue
		currentPUUID := queue[0]
		queue = queue[1:]

		if processed[currentPUUID] {
			continue
		}
		processed[currentPUUID] = true
		playerCount++

		fmt.Printf("\n[Player %d/%d] Processing: %s\n", playerCount, *maxPlayers, currentPUUID[:20]+"...")

		// Fetch match history
		matchIDs, err := client.GetMatchHistory(ctx, currentPUUID, *matchCount)
		if err != nil {
			log.Printf("  Failed to fetch match history: %v", err)
			continue
		}
		fmt.Printf("  Found %d matches\n", len(matchIDs))

		// Process each match
		for j, matchID := range matchIDs {
			// Check if match already exists
			exists, err := database.MatchExists(ctx, matchID)
			if err != nil {
				log.Printf("  Error checking match: %v", err)
				continue
			}
			if exists {
				fmt.Printf("  [%d/%d] Match %s already exists, skipping\n", j+1, len(matchIDs), matchID)
				continue
			}

			fmt.Printf("  [%d/%d] Fetching match %s...\n", j+1, len(matchIDs), matchID)

			// Fetch match details
			match, err := client.GetMatch(ctx, matchID)
			if err != nil {
				log.Printf("    Failed to fetch match: %v", err)
				continue
			}

			// Fetch timeline for build order
			timeline, err := client.GetTimeline(ctx, matchID)
			if err != nil {
				log.Printf("    Failed to fetch timeline: %v", err)
				continue
			}

			// Insert match
			dbMatch := &db.Match{
				MatchID:      matchID,
				GameVersion:  match.Info.GameVersion,
				GameDuration: match.Info.GameDuration,
				GameCreation: match.Info.GameCreation,
			}
			if err := database.InsertMatch(ctx, dbMatch); err != nil {
				log.Printf("    Failed to insert match: %v", err)
				continue
			}

			// Insert participants and discover new players
			for pIdx, participant := range match.Info.Participants {
				participantID := pIdx + 1 // Participant IDs are 1-indexed in timeline

				// Extract build order from timeline
				buildOrder := riot.ExtractBuildOrder(timeline, participantID)

				dbParticipant := &db.Participant{
					MatchID:      matchID,
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
					BuildOrder:   buildOrder,
				}
				if err := database.InsertParticipant(ctx, dbParticipant); err != nil {
					log.Printf("    Failed to insert participant: %v", err)
				}

				// Add new players to queue
				if !discovered[participant.PUUID] {
					discovered[participant.PUUID] = true
					queue = append(queue, participant.PUUID)
				}
			}

			fmt.Printf("    Saved match with %d participants\n", len(match.Info.Participants))
		}

		fmt.Printf("  Queue size: %d, Discovered: %d\n", len(queue), len(discovered))
	}

	// Print summary
	matchCount2, _ := database.GetMatchCount(ctx)
	participantCount, _ := database.GetParticipantCount(ctx)
	fmt.Printf("\n=== Collection Complete ===\n")
	fmt.Printf("Players processed: %d\n", playerCount)
	fmt.Printf("Total matches in database: %d\n", matchCount2)
	fmt.Printf("Total participants: %d\n", participantCount)
}
