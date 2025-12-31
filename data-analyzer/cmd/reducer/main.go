package main

import (
	"bufio"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// CLI flags
var (
	outputDir = flag.String("output-dir", "./export", "Directory to output data.json")
)

const DDRAGON_VERSION = "14.24.1"

// completedItems is a set of item IDs that are completed (not components)
var completedItems map[int]bool

// DDragonItem represents an item from Data Dragon
type DDragonItem struct {
	Name string   `json:"name"`
	Into []string `json:"into,omitempty"` // Items this builds into (as string IDs)
	From []string `json:"from,omitempty"` // Items this is built from (as string IDs)
	Gold struct {
		Total       int  `json:"total"`
		Purchasable bool `json:"purchasable"`
	} `json:"gold"`
	Maps map[string]bool `json:"maps"` // Map availability
}

// loadCompletedItems fetches item.json from Data Dragon and identifies completed items
func loadCompletedItems() error {
	url := fmt.Sprintf("https://ddragon.leagueoflegends.com/cdn/%s/data/en_US/item.json", DDRAGON_VERSION)

	fmt.Printf("Fetching item data from Data Dragon (%s)...\n", DDRAGON_VERSION)

	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("failed to fetch item data: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("Data Dragon returned status %d", resp.StatusCode)
	}

	var result struct {
		Data map[string]DDragonItem `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("failed to parse item data: %w", err)
	}

	completedItems = make(map[int]bool)

	for idStr, item := range result.Data {
		var id int
		fmt.Sscanf(idStr, "%d", &id)

		// Skip items that aren't on Summoner's Rift (map 11)
		if available, exists := item.Maps["11"]; exists && !available {
			continue
		}

		// Completed item = has no "into" (doesn't build into anything else)
		// Also filter out cheap items (< 1000 gold) which are usually components/consumables
		if len(item.Into) == 0 && item.Gold.Total >= 1000 && item.Gold.Purchasable {
			completedItems[id] = true
		}
	}

	fmt.Printf("Loaded %d completed items\n", len(completedItems))
	return nil
}

// isCompletedItem checks if an item is a completed item
func isCompletedItem(itemID int) bool {
	return completedItems[itemID]
}

// RawMatch mirrors the structure from the collector
type RawMatch struct {
	MatchID      string `json:"matchId"`
	GameVersion  string `json:"gameVersion"`
	GameDuration int    `json:"gameDuration"`
	GameCreation int64  `json:"gameCreation"`
	PUUID        string `json:"puuid"`
	GameName     string `json:"gameName,omitempty"`
	TagLine      string `json:"tagLine,omitempty"`
	ChampionID   int    `json:"championId"`
	ChampionName string `json:"championName"`
	TeamPosition string `json:"teamPosition"`
	Win          bool   `json:"win"`
	Item0        int    `json:"item0"`
	Item1        int    `json:"item1"`
	Item2        int    `json:"item2"`
	Item3        int    `json:"item3"`
	Item4        int    `json:"item4"`
	Item5        int    `json:"item5"`
	BuildOrder   []int  `json:"buildOrder"`
}

// ChampionStatsKey is the composite key for champion stats
type ChampionStatsKey struct {
	Patch        string
	ChampionID   int
	TeamPosition string
}

// ChampionStats holds aggregated champion statistics
type ChampionStats struct {
	Wins    int
	Matches int
}

// ItemStatsKey is the composite key for item stats
type ItemStatsKey struct {
	Patch        string
	ChampionID   int
	TeamPosition string
	ItemID       int
}

// ItemStats holds aggregated item statistics
type ItemStats struct {
	Wins    int
	Matches int
}

// MatchupStatsKey is the composite key for matchup stats
type MatchupStatsKey struct {
	Patch           string
	ChampionID      int
	TeamPosition    string
	EnemyChampionID int
}

// MatchupStats holds aggregated matchup statistics
type MatchupStats struct {
	Wins    int
	Matches int
}

// JSON export types
type DataExport struct {
	Patch            string                `json:"patch"`
	GeneratedAt      string                `json:"generatedAt"`
	ChampionStats    []ChampionStatJSON    `json:"championStats"`
	ChampionItems    []ChampionItemJSON    `json:"championItems"`
	ChampionMatchups []ChampionMatchupJSON `json:"championMatchups"`
}

type ChampionStatJSON struct {
	Patch        string `json:"patch"`
	ChampionID   int    `json:"championId"`
	TeamPosition string `json:"teamPosition"`
	Wins         int    `json:"wins"`
	Matches      int    `json:"matches"`
}

type ChampionItemJSON struct {
	Patch        string `json:"patch"`
	ChampionID   int    `json:"championId"`
	TeamPosition string `json:"teamPosition"`
	ItemID       int    `json:"itemId"`
	Wins         int    `json:"wins"`
	Matches      int    `json:"matches"`
}

type ChampionMatchupJSON struct {
	Patch           string `json:"patch"`
	ChampionID      int    `json:"championId"`
	TeamPosition    string `json:"teamPosition"`
	EnemyChampionID int    `json:"enemyChampionId"`
	Wins            int    `json:"wins"`
	Matches         int    `json:"matches"`
}

func main() {
	flag.Parse()

	// Load .env
	envPaths := []string{".env", "../.env", "../../.env", "data-analyzer/.env"}
	for _, path := range envPaths {
		if err := godotenv.Load(path); err == nil {
			fmt.Printf("Loaded .env from: %s\n", path)
			break
		}
	}

	// Get storage path
	storagePath := os.Getenv("BLOB_STORAGE_PATH")
	if storagePath == "" {
		log.Fatal("BLOB_STORAGE_PATH environment variable not set")
	}
	storagePath = strings.Trim(storagePath, "\"")

	warmDir := filepath.Join(storagePath, "warm")
	coldDir := filepath.Join(storagePath, "cold")

	// Ensure cold directory exists
	if err := os.MkdirAll(coldDir, 0755); err != nil {
		log.Fatalf("Failed to create cold directory: %v", err)
	}

	// Load completed items from Data Dragon
	if err := loadCompletedItems(); err != nil {
		log.Fatalf("Failed to load item data: %v", err)
	}

	// Scan warm directory for .jsonl files
	files, err := filepath.Glob(filepath.Join(warmDir, "*.jsonl"))
	if err != nil {
		log.Fatalf("Failed to scan warm directory: %v", err)
	}

	if len(files) == 0 {
		fmt.Println("No files to process in warm directory")
		return
	}

	fmt.Printf("Found %d files to process\n", len(files))

	// Aggregate ALL files together into global maps
	allChampionStats := make(map[ChampionStatsKey]*ChampionStats)
	allItemStats := make(map[ItemStatsKey]*ItemStats)
	allMatchupStats := make(map[MatchupStatsKey]*MatchupStats)
	var detectedPatch string

	// Process each file and accumulate stats
	for i, filePath := range files {
		fmt.Printf("\n[%d/%d] Processing: %s\n", i+1, len(files), filepath.Base(filePath))

		championStats, itemStats, matchupStats, patch, err := aggregateFile(filePath)
		if err != nil {
			log.Printf("  Error processing file: %v", err)
			continue
		}

		// Track the patch (use the last one seen)
		if patch != "" {
			detectedPatch = patch
		}

		// Merge into global maps
		for k, v := range championStats {
			if existing, ok := allChampionStats[k]; ok {
				existing.Wins += v.Wins
				existing.Matches += v.Matches
			} else {
				allChampionStats[k] = v
			}
		}

		for k, v := range itemStats {
			if existing, ok := allItemStats[k]; ok {
				existing.Wins += v.Wins
				existing.Matches += v.Matches
			} else {
				allItemStats[k] = v
			}
		}

		for k, v := range matchupStats {
			if existing, ok := allMatchupStats[k]; ok {
				existing.Wins += v.Wins
				existing.Matches += v.Matches
			} else {
				allMatchupStats[k] = v
			}
		}

		fmt.Printf("  Aggregated: %d champion stats, %d item stats, %d matchup stats\n",
			len(championStats), len(itemStats), len(matchupStats))
	}

	fmt.Printf("\n=== Total Aggregated ===\n")
	fmt.Printf("Champion stats: %d\n", len(allChampionStats))
	fmt.Printf("Item stats: %d\n", len(allItemStats))
	fmt.Printf("Matchup stats: %d\n", len(allMatchupStats))
	fmt.Printf("Detected patch: %s\n", detectedPatch)

	// Export to JSON
	fmt.Printf("\n=== Exporting JSON ===\n")
	if err := exportToJSON(*outputDir, detectedPatch, allChampionStats, allItemStats, allMatchupStats); err != nil {
		log.Fatalf("Failed to export JSON: %v", err)
	}
	fmt.Printf("Exported to: %s\n", *outputDir)

	// Archive files after successful processing
	for _, filePath := range files {
		if err := archiveFile(filePath, coldDir); err != nil {
			log.Printf("Warning: Failed to archive %s: %v", filepath.Base(filePath), err)
		}
	}

	fmt.Println("\n=== Reducer Complete ===")
}

func aggregateFile(filePath string) (map[ChampionStatsKey]*ChampionStats, map[ItemStatsKey]*ItemStats, map[MatchupStatsKey]*MatchupStats, string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, nil, nil, "", err
	}
	defer file.Close()

	championStats := make(map[ChampionStatsKey]*ChampionStats)
	itemStats := make(map[ItemStatsKey]*ItemStats)
	matchupStats := make(map[MatchupStatsKey]*MatchupStats)
	var detectedPatch string

	// First pass: group all participants by matchId
	matchParticipants := make(map[string][]RawMatch)

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Bytes()

		var match RawMatch
		if err := json.Unmarshal(line, &match); err != nil {
			log.Printf("  Warning: failed to parse line %d: %v", lineNum, err)
			continue
		}

		// Skip if no position
		if match.TeamPosition == "" {
			continue
		}

		// Normalize patch version
		patch := normalizePatch(match.GameVersion)
		if detectedPatch == "" {
			detectedPatch = patch
		}

		// Aggregate champion stats
		champKey := ChampionStatsKey{
			Patch:        patch,
			ChampionID:   match.ChampionID,
			TeamPosition: match.TeamPosition,
		}

		if _, exists := championStats[champKey]; !exists {
			championStats[champKey] = &ChampionStats{}
		}
		championStats[champKey].Matches++
		if match.Win {
			championStats[champKey].Wins++
		}

		// Aggregate item stats
		items := deduplicateItems(match.Item0, match.Item1, match.Item2, match.Item3, match.Item4, match.Item5)

		for _, itemID := range items {
			if itemID == 0 || !isCompletedItem(itemID) {
				continue
			}

			itemKey := ItemStatsKey{
				Patch:        patch,
				ChampionID:   match.ChampionID,
				TeamPosition: match.TeamPosition,
				ItemID:       itemID,
			}

			if _, exists := itemStats[itemKey]; !exists {
				itemStats[itemKey] = &ItemStats{}
			}
			itemStats[itemKey].Matches++
			if match.Win {
				itemStats[itemKey].Wins++
			}
		}

		// Group by matchId for matchup calculation
		matchParticipants[match.MatchID] = append(matchParticipants[match.MatchID], match)
	}

	if err := scanner.Err(); err != nil {
		return nil, nil, nil, "", err
	}

	// Second pass: calculate matchups from grouped participants
	for _, participants := range matchParticipants {
		// Group by position
		byPosition := make(map[string][]RawMatch)
		for _, p := range participants {
			byPosition[p.TeamPosition] = append(byPosition[p.TeamPosition], p)
		}

		// For each position, find the two opponents (one winner, one loser)
		for _, posPlayers := range byPosition {
			if len(posPlayers) != 2 {
				continue // Skip if not exactly 2 players in this position
			}

			p1, p2 := posPlayers[0], posPlayers[1]

			// They should be on opposite teams (one won, one lost)
			if p1.Win == p2.Win {
				continue // Same result = probably same team, skip
			}

			patch := normalizePatch(p1.GameVersion)

			// Record matchup for p1 vs p2
			key1 := MatchupStatsKey{
				Patch:           patch,
				ChampionID:      p1.ChampionID,
				TeamPosition:    p1.TeamPosition,
				EnemyChampionID: p2.ChampionID,
			}
			if _, exists := matchupStats[key1]; !exists {
				matchupStats[key1] = &MatchupStats{}
			}
			matchupStats[key1].Matches++
			if p1.Win {
				matchupStats[key1].Wins++
			}

			// Record matchup for p2 vs p1
			key2 := MatchupStatsKey{
				Patch:           patch,
				ChampionID:      p2.ChampionID,
				TeamPosition:    p2.TeamPosition,
				EnemyChampionID: p1.ChampionID,
			}
			if _, exists := matchupStats[key2]; !exists {
				matchupStats[key2] = &MatchupStats{}
			}
			matchupStats[key2].Matches++
			if p2.Win {
				matchupStats[key2].Wins++
			}
		}
	}

	fmt.Printf("  Processed %d lines, %d unique matches\n", lineNum, len(matchParticipants))
	return championStats, itemStats, matchupStats, detectedPatch, nil
}

// normalizePatch truncates version to first two segments (e.g., 14.23.448 -> 14.23)
func normalizePatch(version string) string {
	parts := strings.Split(version, ".")
	if len(parts) >= 2 {
		return parts[0] + "." + parts[1]
	}
	return version
}

// deduplicateItems returns unique item IDs from the inventory
func deduplicateItems(items ...int) []int {
	seen := make(map[int]bool)
	result := make([]int, 0, len(items))

	for _, item := range items {
		if item != 0 && !seen[item] {
			seen[item] = true
			result = append(result, item)
		}
	}
	return result
}

func archiveFile(srcPath, coldDir string) error {
	// Open source file
	src, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer src.Close()

	// Create gzipped destination
	filename := filepath.Base(srcPath) + ".gz"
	dstPath := filepath.Join(coldDir, filename)

	dst, err := os.Create(dstPath)
	if err != nil {
		return err
	}
	defer dst.Close()

	// Write compressed content
	gzWriter := gzip.NewWriter(dst)
	if _, err := io.Copy(gzWriter, src); err != nil {
		os.Remove(dstPath) // Clean up on failure
		return err
	}
	if err := gzWriter.Close(); err != nil {
		os.Remove(dstPath)
		return err
	}

	// Remove original file
	src.Close() // Close before removing on Windows
	if err := os.Remove(srcPath); err != nil {
		return err
	}

	fmt.Printf("  Archived to: %s\n", filename)
	return nil
}

// exportToJSON exports aggregated data to data.json
func exportToJSON(outputDir, patch string,
	championStats map[ChampionStatsKey]*ChampionStats,
	itemStats map[ItemStatsKey]*ItemStats,
	matchupStats map[MatchupStatsKey]*MatchupStats) error {

	// Ensure output directory exists
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Convert maps to JSON arrays
	var champStatsJSON []ChampionStatJSON
	for k, v := range championStats {
		champStatsJSON = append(champStatsJSON, ChampionStatJSON{
			Patch:        k.Patch,
			ChampionID:   k.ChampionID,
			TeamPosition: k.TeamPosition,
			Wins:         v.Wins,
			Matches:      v.Matches,
		})
	}

	var itemStatsJSON []ChampionItemJSON
	for k, v := range itemStats {
		itemStatsJSON = append(itemStatsJSON, ChampionItemJSON{
			Patch:        k.Patch,
			ChampionID:   k.ChampionID,
			TeamPosition: k.TeamPosition,
			ItemID:       k.ItemID,
			Wins:         v.Wins,
			Matches:      v.Matches,
		})
	}

	var matchupStatsJSON []ChampionMatchupJSON
	for k, v := range matchupStats {
		matchupStatsJSON = append(matchupStatsJSON, ChampionMatchupJSON{
			Patch:           k.Patch,
			ChampionID:      k.ChampionID,
			TeamPosition:    k.TeamPosition,
			EnemyChampionID: k.EnemyChampionID,
			Wins:            v.Wins,
			Matches:         v.Matches,
		})
	}

	export := DataExport{
		Patch:            patch,
		GeneratedAt:      time.Now().UTC().Format(time.RFC3339),
		ChampionStats:    champStatsJSON,
		ChampionItems:    itemStatsJSON,
		ChampionMatchups: matchupStatsJSON,
	}

	// Write data.json
	dataPath := filepath.Join(outputDir, "data.json")
	dataFile, err := os.Create(dataPath)
	if err != nil {
		return fmt.Errorf("failed to create data.json: %w", err)
	}
	defer dataFile.Close()

	// Write to file and compute SHA256 simultaneously
	hasher := sha256.New()
	multiWriter := io.MultiWriter(dataFile, hasher)

	encoder := json.NewEncoder(multiWriter)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(export); err != nil {
		return fmt.Errorf("failed to write data.json: %w", err)
	}

	dataSha256 := hex.EncodeToString(hasher.Sum(nil))

	fmt.Printf("  Wrote data.json: %d champion stats, %d item stats, %d matchup stats\n",
		len(champStatsJSON), len(itemStatsJSON), len(matchupStatsJSON))
	fmt.Printf("  SHA256: %s\n", dataSha256)
	return nil
}
