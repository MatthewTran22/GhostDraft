package collector

import (
	"bufio"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"

	"data-analyzer/internal/storage"

	json "github.com/goccy/go-json"
)

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

// ItemSlotStatsKey is the composite key for item slot stats
type ItemSlotStatsKey struct {
	Patch        string
	ChampionID   int
	TeamPosition string
	ItemID       int
	BuildSlot    int // 1-6 for first through sixth completed item
}

// ItemSlotStats holds aggregated item slot statistics
type ItemSlotStats struct {
	Wins    int
	Matches int
}

// AggData holds all aggregated statistics from warm files
type AggData struct {
	ChampionStats  map[ChampionStatsKey]*ChampionStats
	ItemStats      map[ItemStatsKey]*ItemStats
	ItemSlotStats  map[ItemSlotStatsKey]*ItemSlotStats
	MatchupStats   map[MatchupStatsKey]*MatchupStats
	DetectedPatch  string
	FilesProcessed int
	TotalRecords   int
}

// ItemFilter is a function that determines if an item should be included in stats
type ItemFilter func(itemID int) bool

// AggregateWarmFiles reads all JSONL files from the warm directory and aggregates stats
func AggregateWarmFiles(warmDir string, itemFilter ItemFilter) (*AggData, error) {
	agg := &AggData{
		ChampionStats: make(map[ChampionStatsKey]*ChampionStats),
		ItemStats:     make(map[ItemStatsKey]*ItemStats),
		ItemSlotStats: make(map[ItemSlotStatsKey]*ItemSlotStats),
		MatchupStats:  make(map[MatchupStatsKey]*MatchupStats),
	}

	// Scan warm directory for .jsonl files
	files, err := filepath.Glob(filepath.Join(warmDir, "*.jsonl"))
	if err != nil {
		return nil, err
	}

	if len(files) == 0 {
		return agg, nil
	}

	// Process each file and accumulate stats
	for _, filePath := range files {
		championStats, itemStats, itemSlotStats, matchupStats, patch, records, err := aggregateFile(filePath, itemFilter)
		if err != nil {
			continue // Skip files with errors
		}

		agg.FilesProcessed++
		agg.TotalRecords += records

		// Track the patch (use the last one seen)
		if patch != "" {
			agg.DetectedPatch = patch
		}

		// Merge champion stats
		for k, v := range championStats {
			if existing, ok := agg.ChampionStats[k]; ok {
				existing.Wins += v.Wins
				existing.Matches += v.Matches
			} else {
				agg.ChampionStats[k] = v
			}
		}

		// Merge item stats
		for k, v := range itemStats {
			if existing, ok := agg.ItemStats[k]; ok {
				existing.Wins += v.Wins
				existing.Matches += v.Matches
			} else {
				agg.ItemStats[k] = v
			}
		}

		// Merge item slot stats
		for k, v := range itemSlotStats {
			if existing, ok := agg.ItemSlotStats[k]; ok {
				existing.Wins += v.Wins
				existing.Matches += v.Matches
			} else {
				agg.ItemSlotStats[k] = v
			}
		}

		// Merge matchup stats
		for k, v := range matchupStats {
			if existing, ok := agg.MatchupStats[k]; ok {
				existing.Wins += v.Wins
				existing.Matches += v.Matches
			} else {
				agg.MatchupStats[k] = v
			}
		}
	}

	return agg, nil
}

// aggregateFile processes a single JSONL file and returns per-file stats
func aggregateFile(filePath string, itemFilter ItemFilter) (
	map[ChampionStatsKey]*ChampionStats,
	map[ItemStatsKey]*ItemStats,
	map[ItemSlotStatsKey]*ItemSlotStats,
	map[MatchupStatsKey]*MatchupStats,
	string, int, error,
) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, nil, nil, nil, "", 0, err
	}
	defer file.Close()

	championStats := make(map[ChampionStatsKey]*ChampionStats)
	itemStats := make(map[ItemStatsKey]*ItemStats)
	itemSlotStats := make(map[ItemSlotStatsKey]*ItemSlotStats)
	matchupStats := make(map[MatchupStatsKey]*MatchupStats)
	var detectedPatch string

	// First pass: group all participants by matchId
	matchParticipants := make(map[string][]storage.RawMatch)

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	recordCount := 0
	for scanner.Scan() {
		line := scanner.Bytes()

		var match storage.RawMatch
		if err := json.Unmarshal(line, &match); err != nil {
			continue
		}

		recordCount++

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

		// ITEM STATS: Always use final inventory (item0-5) for 100% of matches
		finalItems := []int{match.Item0, match.Item1, match.Item2, match.Item3, match.Item4, match.Item5}
		seenItems := make(map[int]bool)
		for _, itemID := range finalItems {
			// Skip empty, duplicates, and non-completed items
			if itemID == 0 || seenItems[itemID] || !itemFilter(itemID) {
				continue
			}
			seenItems[itemID] = true

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

		// ITEM SLOT STATS: Only process when BuildOrder exists (sampled matches)
		if len(match.BuildOrder) > 0 {
			seenSlotItems := make(map[int]bool)
			buildSlot := 0
			for _, itemID := range match.BuildOrder {
				// Skip empty, duplicates, and non-completed items
				if itemID == 0 || seenSlotItems[itemID] || !itemFilter(itemID) {
					continue
				}
				seenSlotItems[itemID] = true
				buildSlot++

				// Only track slots 1-6
				if buildSlot <= 6 {
					slotKey := ItemSlotStatsKey{
						Patch:        patch,
						ChampionID:   match.ChampionID,
						TeamPosition: match.TeamPosition,
						ItemID:       itemID,
						BuildSlot:    buildSlot,
					}

					if _, exists := itemSlotStats[slotKey]; !exists {
						itemSlotStats[slotKey] = &ItemSlotStats{}
					}
					itemSlotStats[slotKey].Matches++
					if match.Win {
						itemSlotStats[slotKey].Wins++
					}
				}
			}
		}

		// Group by matchId for matchup calculation
		matchParticipants[match.MatchID] = append(matchParticipants[match.MatchID], match)
	}

	if err := scanner.Err(); err != nil {
		return nil, nil, nil, nil, "", 0, err
	}

	// Second pass: calculate matchups from grouped participants
	for _, participants := range matchParticipants {
		// Group by position
		byPosition := make(map[string][]storage.RawMatch)
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

	return championStats, itemStats, itemSlotStats, matchupStats, detectedPatch, recordCount, nil
}

// normalizePatch truncates version to first two segments (e.g., 14.23.448 -> 14.23)
func normalizePatch(version string) string {
	parts := strings.Split(version, ".")
	if len(parts) >= 2 {
		return parts[0] + "." + parts[1]
	}
	return version
}

// ArchiveWarmToCold moves all .jsonl files from warm to cold with gzip compression.
// Returns the number of files archived.
func ArchiveWarmToCold(warmDir, coldDir string) (int, error) {
	// Ensure cold directory exists
	if err := os.MkdirAll(coldDir, 0755); err != nil {
		return 0, err
	}

	// Scan warm directory for .jsonl files only
	files, err := filepath.Glob(filepath.Join(warmDir, "*.jsonl"))
	if err != nil {
		return 0, err
	}

	if len(files) == 0 {
		return 0, nil
	}

	archived := 0
	for _, srcPath := range files {
		if err := archiveFile(srcPath, coldDir); err != nil {
			return archived, err
		}
		archived++
	}

	return archived, nil
}

// archiveFile compresses a single file to cold directory and removes the original
func archiveFile(srcPath, coldDir string) error {
	// Open source file
	src, err := os.Open(srcPath)
	if err != nil {
		return err
	}

	// Create gzipped destination
	filename := filepath.Base(srcPath) + ".gz"
	dstPath := filepath.Join(coldDir, filename)

	dst, err := os.Create(dstPath)
	if err != nil {
		src.Close()
		return err
	}

	// Write compressed content
	gzWriter := gzip.NewWriter(dst)
	if _, err := io.Copy(gzWriter, src); err != nil {
		gzWriter.Close()
		dst.Close()
		src.Close()
		os.Remove(dstPath) // Clean up on failure
		return err
	}
	if err := gzWriter.Close(); err != nil {
		dst.Close()
		src.Close()
		os.Remove(dstPath)
		return err
	}

	// Close files before removing (required on Windows)
	dst.Close()
	src.Close()

	// Remove original file
	if err := os.Remove(srcPath); err != nil {
		return err
	}

	return nil
}
