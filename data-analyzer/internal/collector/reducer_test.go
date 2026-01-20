package collector

import (
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// Test 3.1: Aggregate warm files to memory
func TestAggregateWarmFiles_Basic(t *testing.T) {
	// Setup temp directory with sample JSONL files
	tempDir := t.TempDir()
	warmDir := filepath.Join(tempDir, "warm")
	if err := os.MkdirAll(warmDir, 0755); err != nil {
		t.Fatalf("Failed to create warm directory: %v", err)
	}

	// Create sample JSONL file with test data
	// Two matches: one with Ahri mid winning, one with Ahri mid losing
	// This should result in: 2 matches, 1 win for Ahri MID
	sampleData := `{"matchId":"NA1_1","gameVersion":"15.24.1","gameDuration":1800,"gameCreation":1700000000000,"puuid":"p1","championId":103,"championName":"Ahri","teamPosition":"MIDDLE","win":true,"item0":3089,"item1":3157,"item2":3020,"item3":0,"item4":0,"item5":0}
{"matchId":"NA1_1","gameVersion":"15.24.1","gameDuration":1800,"gameCreation":1700000000000,"puuid":"p2","championId":238,"championName":"Zed","teamPosition":"MIDDLE","win":false,"item0":3142,"item1":3071,"item2":3020,"item3":0,"item4":0,"item5":0}
{"matchId":"NA1_2","gameVersion":"15.24.1","gameDuration":2100,"gameCreation":1700001000000,"puuid":"p1","championId":103,"championName":"Ahri","teamPosition":"MIDDLE","win":false,"item0":3089,"item1":3020,"item2":0,"item3":0,"item4":0,"item5":0}
{"matchId":"NA1_2","gameVersion":"15.24.1","gameDuration":2100,"gameCreation":1700001000000,"puuid":"p3","championId":7,"championName":"LeBlanc","teamPosition":"MIDDLE","win":true,"item0":3089,"item1":3157,"item2":3020,"item3":0,"item4":0,"item5":0}
`

	jsonlPath := filepath.Join(warmDir, "test_001.jsonl")
	if err := os.WriteFile(jsonlPath, []byte(sampleData), 0644); err != nil {
		t.Fatalf("Failed to write sample JSONL: %v", err)
	}

	// Create a mock item filter that accepts items >= 3000
	itemFilter := func(itemID int) bool {
		return itemID >= 3000
	}

	// Call aggregator
	agg, err := AggregateWarmFiles(warmDir, itemFilter)
	if err != nil {
		t.Fatalf("AggregateWarmFiles failed: %v", err)
	}

	// Verify champion stats
	// Ahri MID: 2 matches, 1 win
	ahriKey := ChampionStatsKey{Patch: "15.24", ChampionID: 103, TeamPosition: "MIDDLE"}
	ahriStats, ok := agg.ChampionStats[ahriKey]
	if !ok {
		t.Errorf("Expected Ahri MIDDLE stats to exist")
	} else {
		if ahriStats.Matches != 2 {
			t.Errorf("Ahri MIDDLE matches: got %d, want 2", ahriStats.Matches)
		}
		if ahriStats.Wins != 1 {
			t.Errorf("Ahri MIDDLE wins: got %d, want 1", ahriStats.Wins)
		}
	}

	// Zed MID: 1 match, 0 wins
	zedKey := ChampionStatsKey{Patch: "15.24", ChampionID: 238, TeamPosition: "MIDDLE"}
	zedStats, ok := agg.ChampionStats[zedKey]
	if !ok {
		t.Errorf("Expected Zed MIDDLE stats to exist")
	} else {
		if zedStats.Matches != 1 {
			t.Errorf("Zed MIDDLE matches: got %d, want 1", zedStats.Matches)
		}
		if zedStats.Wins != 0 {
			t.Errorf("Zed MIDDLE wins: got %d, want 0", zedStats.Wins)
		}
	}

	// LeBlanc MID: 1 match, 1 win
	lbKey := ChampionStatsKey{Patch: "15.24", ChampionID: 7, TeamPosition: "MIDDLE"}
	lbStats, ok := agg.ChampionStats[lbKey]
	if !ok {
		t.Errorf("Expected LeBlanc MIDDLE stats to exist")
	} else {
		if lbStats.Matches != 1 {
			t.Errorf("LeBlanc MIDDLE matches: got %d, want 1", lbStats.Matches)
		}
		if lbStats.Wins != 1 {
			t.Errorf("LeBlanc MIDDLE wins: got %d, want 1", lbStats.Wins)
		}
	}

	// Verify detected patch
	if agg.DetectedPatch != "15.24" {
		t.Errorf("DetectedPatch: got %q, want %q", agg.DetectedPatch, "15.24")
	}
}

// Test 3.1 continued: Verify item stats aggregation
func TestAggregateWarmFiles_ItemStats(t *testing.T) {
	tempDir := t.TempDir()
	warmDir := filepath.Join(tempDir, "warm")
	if err := os.MkdirAll(warmDir, 0755); err != nil {
		t.Fatalf("Failed to create warm directory: %v", err)
	}

	// Ahri with Rabadon (3089) wins, Ahri with Rabadon loses
	// Item 3089 on Ahri MID: 2 matches, 1 win
	sampleData := `{"matchId":"NA1_1","gameVersion":"15.24.1","gameDuration":1800,"gameCreation":1700000000000,"puuid":"p1","championId":103,"championName":"Ahri","teamPosition":"MIDDLE","win":true,"item0":3089,"item1":3157,"item2":0,"item3":0,"item4":0,"item5":0}
{"matchId":"NA1_1","gameVersion":"15.24.1","gameDuration":1800,"gameCreation":1700000000000,"puuid":"p2","championId":238,"championName":"Zed","teamPosition":"MIDDLE","win":false,"item0":3142,"item1":0,"item2":0,"item3":0,"item4":0,"item5":0}
{"matchId":"NA1_2","gameVersion":"15.24.1","gameDuration":2100,"gameCreation":1700001000000,"puuid":"p1","championId":103,"championName":"Ahri","teamPosition":"MIDDLE","win":false,"item0":3089,"item1":0,"item2":0,"item3":0,"item4":0,"item5":0}
{"matchId":"NA1_2","gameVersion":"15.24.1","gameDuration":2100,"gameCreation":1700001000000,"puuid":"p3","championId":7,"championName":"LeBlanc","teamPosition":"MIDDLE","win":true,"item0":3157,"item1":0,"item2":0,"item3":0,"item4":0,"item5":0}
`

	jsonlPath := filepath.Join(warmDir, "test_001.jsonl")
	if err := os.WriteFile(jsonlPath, []byte(sampleData), 0644); err != nil {
		t.Fatalf("Failed to write sample JSONL: %v", err)
	}

	// Accept all items >= 3000
	itemFilter := func(itemID int) bool {
		return itemID >= 3000
	}

	agg, err := AggregateWarmFiles(warmDir, itemFilter)
	if err != nil {
		t.Fatalf("AggregateWarmFiles failed: %v", err)
	}

	// Verify Ahri + Rabadon (3089) stats
	ahriRabadonKey := ItemStatsKey{
		Patch:        "15.24",
		ChampionID:   103,
		TeamPosition: "MIDDLE",
		ItemID:       3089,
	}
	itemStats, ok := agg.ItemStats[ahriRabadonKey]
	if !ok {
		t.Errorf("Expected Ahri MIDDLE with Rabadon (3089) stats to exist")
	} else {
		if itemStats.Matches != 2 {
			t.Errorf("Ahri+Rabadon matches: got %d, want 2", itemStats.Matches)
		}
		if itemStats.Wins != 1 {
			t.Errorf("Ahri+Rabadon wins: got %d, want 1", itemStats.Wins)
		}
	}
}

// Test 3.1 continued: Verify matchup stats aggregation
func TestAggregateWarmFiles_MatchupStats(t *testing.T) {
	tempDir := t.TempDir()
	warmDir := filepath.Join(tempDir, "warm")
	if err := os.MkdirAll(warmDir, 0755); err != nil {
		t.Fatalf("Failed to create warm directory: %v", err)
	}

	// Match 1: Ahri MID wins vs Zed MID
	// Match 2: Ahri MID loses vs LeBlanc MID
	sampleData := `{"matchId":"NA1_1","gameVersion":"15.24.1","gameDuration":1800,"gameCreation":1700000000000,"puuid":"p1","championId":103,"championName":"Ahri","teamPosition":"MIDDLE","win":true,"item0":3089,"item1":0,"item2":0,"item3":0,"item4":0,"item5":0}
{"matchId":"NA1_1","gameVersion":"15.24.1","gameDuration":1800,"gameCreation":1700000000000,"puuid":"p2","championId":238,"championName":"Zed","teamPosition":"MIDDLE","win":false,"item0":3142,"item1":0,"item2":0,"item3":0,"item4":0,"item5":0}
{"matchId":"NA1_2","gameVersion":"15.24.1","gameDuration":2100,"gameCreation":1700001000000,"puuid":"p1","championId":103,"championName":"Ahri","teamPosition":"MIDDLE","win":false,"item0":3089,"item1":0,"item2":0,"item3":0,"item4":0,"item5":0}
{"matchId":"NA1_2","gameVersion":"15.24.1","gameDuration":2100,"gameCreation":1700001000000,"puuid":"p3","championId":7,"championName":"LeBlanc","teamPosition":"MIDDLE","win":true,"item0":3157,"item1":0,"item2":0,"item3":0,"item4":0,"item5":0}
`

	jsonlPath := filepath.Join(warmDir, "test_001.jsonl")
	if err := os.WriteFile(jsonlPath, []byte(sampleData), 0644); err != nil {
		t.Fatalf("Failed to write sample JSONL: %v", err)
	}

	itemFilter := func(itemID int) bool { return itemID >= 3000 }

	agg, err := AggregateWarmFiles(warmDir, itemFilter)
	if err != nil {
		t.Fatalf("AggregateWarmFiles failed: %v", err)
	}

	// Ahri vs Zed: 1 match, 1 win (Ahri won)
	ahriVsZedKey := MatchupStatsKey{
		Patch:           "15.24",
		ChampionID:      103,
		TeamPosition:    "MIDDLE",
		EnemyChampionID: 238,
	}
	matchupStats, ok := agg.MatchupStats[ahriVsZedKey]
	if !ok {
		t.Errorf("Expected Ahri vs Zed matchup stats to exist")
	} else {
		if matchupStats.Matches != 1 {
			t.Errorf("Ahri vs Zed matches: got %d, want 1", matchupStats.Matches)
		}
		if matchupStats.Wins != 1 {
			t.Errorf("Ahri vs Zed wins: got %d, want 1", matchupStats.Wins)
		}
	}

	// Zed vs Ahri: 1 match, 0 wins (Zed lost)
	zedVsAhriKey := MatchupStatsKey{
		Patch:           "15.24",
		ChampionID:      238,
		TeamPosition:    "MIDDLE",
		EnemyChampionID: 103,
	}
	matchupStats, ok = agg.MatchupStats[zedVsAhriKey]
	if !ok {
		t.Errorf("Expected Zed vs Ahri matchup stats to exist")
	} else {
		if matchupStats.Matches != 1 {
			t.Errorf("Zed vs Ahri matches: got %d, want 1", matchupStats.Matches)
		}
		if matchupStats.Wins != 0 {
			t.Errorf("Zed vs Ahri wins: got %d, want 0", matchupStats.Wins)
		}
	}

	// Ahri vs LeBlanc: 1 match, 0 wins (Ahri lost)
	ahriVsLBKey := MatchupStatsKey{
		Patch:           "15.24",
		ChampionID:      103,
		TeamPosition:    "MIDDLE",
		EnemyChampionID: 7,
	}
	matchupStats, ok = agg.MatchupStats[ahriVsLBKey]
	if !ok {
		t.Errorf("Expected Ahri vs LeBlanc matchup stats to exist")
	} else {
		if matchupStats.Matches != 1 {
			t.Errorf("Ahri vs LeBlanc matches: got %d, want 1", matchupStats.Matches)
		}
		if matchupStats.Wins != 0 {
			t.Errorf("Ahri vs LeBlanc wins: got %d, want 0", matchupStats.Wins)
		}
	}
}

// Test 3.1 continued: Verify item slot stats (buildOrder) aggregation
func TestAggregateWarmFiles_ItemSlotStats(t *testing.T) {
	tempDir := t.TempDir()
	warmDir := filepath.Join(tempDir, "warm")
	if err := os.MkdirAll(warmDir, 0755); err != nil {
		t.Fatalf("Failed to create warm directory: %v", err)
	}

	// Ahri with buildOrder: Rabadon first, Zhonya second
	sampleData := `{"matchId":"NA1_1","gameVersion":"15.24.1","gameDuration":1800,"gameCreation":1700000000000,"puuid":"p1","championId":103,"championName":"Ahri","teamPosition":"MIDDLE","win":true,"item0":3089,"item1":3157,"item2":0,"item3":0,"item4":0,"item5":0,"buildOrder":[3089,3157]}
{"matchId":"NA1_1","gameVersion":"15.24.1","gameDuration":1800,"gameCreation":1700000000000,"puuid":"p2","championId":238,"championName":"Zed","teamPosition":"MIDDLE","win":false,"item0":3142,"item1":0,"item2":0,"item3":0,"item4":0,"item5":0}
`

	jsonlPath := filepath.Join(warmDir, "test_001.jsonl")
	if err := os.WriteFile(jsonlPath, []byte(sampleData), 0644); err != nil {
		t.Fatalf("Failed to write sample JSONL: %v", err)
	}

	itemFilter := func(itemID int) bool { return itemID >= 3000 }

	agg, err := AggregateWarmFiles(warmDir, itemFilter)
	if err != nil {
		t.Fatalf("AggregateWarmFiles failed: %v", err)
	}

	// Ahri slot 1 = Rabadon (3089): 1 match, 1 win
	ahriSlot1Key := ItemSlotStatsKey{
		Patch:        "15.24",
		ChampionID:   103,
		TeamPosition: "MIDDLE",
		ItemID:       3089,
		BuildSlot:    1,
	}
	slotStats, ok := agg.ItemSlotStats[ahriSlot1Key]
	if !ok {
		t.Errorf("Expected Ahri MIDDLE slot 1 Rabadon stats to exist")
	} else {
		if slotStats.Matches != 1 {
			t.Errorf("Ahri slot 1 Rabadon matches: got %d, want 1", slotStats.Matches)
		}
		if slotStats.Wins != 1 {
			t.Errorf("Ahri slot 1 Rabadon wins: got %d, want 1", slotStats.Wins)
		}
	}

	// Ahri slot 2 = Zhonya (3157): 1 match, 1 win
	ahriSlot2Key := ItemSlotStatsKey{
		Patch:        "15.24",
		ChampionID:   103,
		TeamPosition: "MIDDLE",
		ItemID:       3157,
		BuildSlot:    2,
	}
	slotStats, ok = agg.ItemSlotStats[ahriSlot2Key]
	if !ok {
		t.Errorf("Expected Ahri MIDDLE slot 2 Zhonya stats to exist")
	} else {
		if slotStats.Matches != 1 {
			t.Errorf("Ahri slot 2 Zhonya matches: got %d, want 1", slotStats.Matches)
		}
		if slotStats.Wins != 1 {
			t.Errorf("Ahri slot 2 Zhonya wins: got %d, want 1", slotStats.Wins)
		}
	}
}

// Test 3.1 continued: Multiple files aggregation
func TestAggregateWarmFiles_MultipleFiles(t *testing.T) {
	tempDir := t.TempDir()
	warmDir := filepath.Join(tempDir, "warm")
	if err := os.MkdirAll(warmDir, 0755); err != nil {
		t.Fatalf("Failed to create warm directory: %v", err)
	}

	// File 1: Ahri wins
	file1Data := `{"matchId":"NA1_1","gameVersion":"15.24.1","gameDuration":1800,"gameCreation":1700000000000,"puuid":"p1","championId":103,"championName":"Ahri","teamPosition":"MIDDLE","win":true,"item0":3089,"item1":0,"item2":0,"item3":0,"item4":0,"item5":0}
{"matchId":"NA1_1","gameVersion":"15.24.1","gameDuration":1800,"gameCreation":1700000000000,"puuid":"p2","championId":238,"championName":"Zed","teamPosition":"MIDDLE","win":false,"item0":3142,"item1":0,"item2":0,"item3":0,"item4":0,"item5":0}
`

	// File 2: Ahri wins again
	file2Data := `{"matchId":"NA1_2","gameVersion":"15.24.1","gameDuration":2100,"gameCreation":1700001000000,"puuid":"p1","championId":103,"championName":"Ahri","teamPosition":"MIDDLE","win":true,"item0":3089,"item1":0,"item2":0,"item3":0,"item4":0,"item5":0}
{"matchId":"NA1_2","gameVersion":"15.24.1","gameDuration":2100,"gameCreation":1700001000000,"puuid":"p3","championId":7,"championName":"LeBlanc","teamPosition":"MIDDLE","win":false,"item0":3157,"item1":0,"item2":0,"item3":0,"item4":0,"item5":0}
`

	if err := os.WriteFile(filepath.Join(warmDir, "test_001.jsonl"), []byte(file1Data), 0644); err != nil {
		t.Fatalf("Failed to write file1: %v", err)
	}
	if err := os.WriteFile(filepath.Join(warmDir, "test_002.jsonl"), []byte(file2Data), 0644); err != nil {
		t.Fatalf("Failed to write file2: %v", err)
	}

	itemFilter := func(itemID int) bool { return itemID >= 3000 }

	agg, err := AggregateWarmFiles(warmDir, itemFilter)
	if err != nil {
		t.Fatalf("AggregateWarmFiles failed: %v", err)
	}

	// Ahri MID: 2 matches (from 2 files), 2 wins
	ahriKey := ChampionStatsKey{Patch: "15.24", ChampionID: 103, TeamPosition: "MIDDLE"}
	ahriStats, ok := agg.ChampionStats[ahriKey]
	if !ok {
		t.Errorf("Expected Ahri MIDDLE stats to exist")
	} else {
		if ahriStats.Matches != 2 {
			t.Errorf("Ahri MIDDLE matches: got %d, want 2", ahriStats.Matches)
		}
		if ahriStats.Wins != 2 {
			t.Errorf("Ahri MIDDLE wins: got %d, want 2", ahriStats.Wins)
		}
	}

	// Verify files processed count
	if agg.FilesProcessed != 2 {
		t.Errorf("FilesProcessed: got %d, want 2", agg.FilesProcessed)
	}
}

// Test 3.1 continued: Empty warm directory
func TestAggregateWarmFiles_EmptyDirectory(t *testing.T) {
	tempDir := t.TempDir()
	warmDir := filepath.Join(tempDir, "warm")
	if err := os.MkdirAll(warmDir, 0755); err != nil {
		t.Fatalf("Failed to create warm directory: %v", err)
	}

	itemFilter := func(itemID int) bool { return true }

	agg, err := AggregateWarmFiles(warmDir, itemFilter)
	if err != nil {
		t.Fatalf("AggregateWarmFiles failed: %v", err)
	}

	if len(agg.ChampionStats) != 0 {
		t.Errorf("Expected empty champion stats for empty directory")
	}
	if agg.FilesProcessed != 0 {
		t.Errorf("FilesProcessed: got %d, want 0", agg.FilesProcessed)
	}
}

// Test 3.1 continued: Skip records without TeamPosition
func TestAggregateWarmFiles_SkipsEmptyPosition(t *testing.T) {
	tempDir := t.TempDir()
	warmDir := filepath.Join(tempDir, "warm")
	if err := os.MkdirAll(warmDir, 0755); err != nil {
		t.Fatalf("Failed to create warm directory: %v", err)
	}

	// Record with empty teamPosition should be skipped
	sampleData := `{"matchId":"NA1_1","gameVersion":"15.24.1","gameDuration":1800,"gameCreation":1700000000000,"puuid":"p1","championId":103,"championName":"Ahri","teamPosition":"","win":true,"item0":3089,"item1":0,"item2":0,"item3":0,"item4":0,"item5":0}
{"matchId":"NA1_1","gameVersion":"15.24.1","gameDuration":1800,"gameCreation":1700000000000,"puuid":"p2","championId":238,"championName":"Zed","teamPosition":"MIDDLE","win":false,"item0":3142,"item1":0,"item2":0,"item3":0,"item4":0,"item5":0}
`

	jsonlPath := filepath.Join(warmDir, "test_001.jsonl")
	if err := os.WriteFile(jsonlPath, []byte(sampleData), 0644); err != nil {
		t.Fatalf("Failed to write sample JSONL: %v", err)
	}

	itemFilter := func(itemID int) bool { return itemID >= 3000 }

	agg, err := AggregateWarmFiles(warmDir, itemFilter)
	if err != nil {
		t.Fatalf("AggregateWarmFiles failed: %v", err)
	}

	// Ahri should not be counted (empty teamPosition)
	ahriKey := ChampionStatsKey{Patch: "15.24", ChampionID: 103, TeamPosition: ""}
	if _, ok := agg.ChampionStats[ahriKey]; ok {
		t.Errorf("Expected Ahri with empty teamPosition to be skipped")
	}

	// Only Zed should be counted
	if len(agg.ChampionStats) != 1 {
		t.Errorf("Expected 1 champion stat, got %d", len(agg.ChampionStats))
	}
}

// =============================================================================
// Test 3.2: Archive warm to cold with gzip
// =============================================================================

// Test 3.2: Basic archive behavior
func TestArchiveWarmToCold_Basic(t *testing.T) {
	tempDir := t.TempDir()
	warmDir := filepath.Join(tempDir, "warm")
	coldDir := filepath.Join(tempDir, "cold")

	if err := os.MkdirAll(warmDir, 0755); err != nil {
		t.Fatalf("Failed to create warm directory: %v", err)
	}
	if err := os.MkdirAll(coldDir, 0755); err != nil {
		t.Fatalf("Failed to create cold directory: %v", err)
	}

	// Create sample files in warm
	file1Content := "test data line 1\ntest data line 2\n"
	file2Content := "another file content\n"

	if err := os.WriteFile(filepath.Join(warmDir, "test_001.jsonl"), []byte(file1Content), 0644); err != nil {
		t.Fatalf("Failed to write file1: %v", err)
	}
	if err := os.WriteFile(filepath.Join(warmDir, "test_002.jsonl"), []byte(file2Content), 0644); err != nil {
		t.Fatalf("Failed to write file2: %v", err)
	}

	// Archive
	archived, err := ArchiveWarmToCold(warmDir, coldDir)
	if err != nil {
		t.Fatalf("ArchiveWarmToCold failed: %v", err)
	}

	// Verify archived count
	if archived != 2 {
		t.Errorf("Archived count: got %d, want 2", archived)
	}

	// Verify warm directory is empty
	warmFiles, _ := filepath.Glob(filepath.Join(warmDir, "*.jsonl"))
	if len(warmFiles) != 0 {
		t.Errorf("Warm directory should be empty, found %d files", len(warmFiles))
	}

	// Verify cold directory has gzipped files
	coldFiles, _ := filepath.Glob(filepath.Join(coldDir, "*.jsonl.gz"))
	if len(coldFiles) != 2 {
		t.Errorf("Cold directory should have 2 files, found %d", len(coldFiles))
	}

	// Verify file content can be decompressed
	for _, coldFile := range coldFiles {
		content, err := readGzipFile(coldFile)
		if err != nil {
			t.Errorf("Failed to read gzip file %s: %v", filepath.Base(coldFile), err)
		}
		// Should match one of the original contents
		if content != file1Content && content != file2Content {
			t.Errorf("Decompressed content doesn't match original")
		}
	}
}

// Test 3.2: Archive with empty warm directory
func TestArchiveWarmToCold_EmptyWarm(t *testing.T) {
	tempDir := t.TempDir()
	warmDir := filepath.Join(tempDir, "warm")
	coldDir := filepath.Join(tempDir, "cold")

	if err := os.MkdirAll(warmDir, 0755); err != nil {
		t.Fatalf("Failed to create warm directory: %v", err)
	}
	if err := os.MkdirAll(coldDir, 0755); err != nil {
		t.Fatalf("Failed to create cold directory: %v", err)
	}

	archived, err := ArchiveWarmToCold(warmDir, coldDir)
	if err != nil {
		t.Fatalf("ArchiveWarmToCold failed: %v", err)
	}

	if archived != 0 {
		t.Errorf("Archived count: got %d, want 0", archived)
	}
}

// Test 3.2: Archive preserves data integrity
func TestArchiveWarmToCold_DataIntegrity(t *testing.T) {
	tempDir := t.TempDir()
	warmDir := filepath.Join(tempDir, "warm")
	coldDir := filepath.Join(tempDir, "cold")

	if err := os.MkdirAll(warmDir, 0755); err != nil {
		t.Fatalf("Failed to create warm directory: %v", err)
	}
	if err := os.MkdirAll(coldDir, 0755); err != nil {
		t.Fatalf("Failed to create cold directory: %v", err)
	}

	// Create a larger file with JSON lines
	var content string
	for i := 0; i < 1000; i++ {
		content += `{"matchId":"NA1_` + string(rune('0'+i%10)) + `","gameVersion":"15.24.1","win":true}` + "\n"
	}

	if err := os.WriteFile(filepath.Join(warmDir, "large_001.jsonl"), []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write large file: %v", err)
	}

	// Archive
	_, err := ArchiveWarmToCold(warmDir, coldDir)
	if err != nil {
		t.Fatalf("ArchiveWarmToCold failed: %v", err)
	}

	// Verify decompressed content matches original
	decompressed, err := readGzipFile(filepath.Join(coldDir, "large_001.jsonl.gz"))
	if err != nil {
		t.Fatalf("Failed to read gzip file: %v", err)
	}

	if decompressed != content {
		t.Errorf("Decompressed content doesn't match original (%d vs %d bytes)", len(decompressed), len(content))
	}
}

// Test 3.2: Archive only processes .jsonl files
func TestArchiveWarmToCold_OnlyJsonl(t *testing.T) {
	tempDir := t.TempDir()
	warmDir := filepath.Join(tempDir, "warm")
	coldDir := filepath.Join(tempDir, "cold")

	if err := os.MkdirAll(warmDir, 0755); err != nil {
		t.Fatalf("Failed to create warm directory: %v", err)
	}
	if err := os.MkdirAll(coldDir, 0755); err != nil {
		t.Fatalf("Failed to create cold directory: %v", err)
	}

	// Create various file types
	os.WriteFile(filepath.Join(warmDir, "test_001.jsonl"), []byte("valid jsonl"), 0644)
	os.WriteFile(filepath.Join(warmDir, "test.txt"), []byte("text file"), 0644)
	os.WriteFile(filepath.Join(warmDir, "test.json"), []byte("json file"), 0644)

	archived, err := ArchiveWarmToCold(warmDir, coldDir)
	if err != nil {
		t.Fatalf("ArchiveWarmToCold failed: %v", err)
	}

	// Should only archive the .jsonl file
	if archived != 1 {
		t.Errorf("Archived count: got %d, want 1", archived)
	}

	// .txt and .json files should remain in warm
	txtExists := fileExists(filepath.Join(warmDir, "test.txt"))
	jsonExists := fileExists(filepath.Join(warmDir, "test.json"))
	if !txtExists || !jsonExists {
		t.Errorf("Non-jsonl files should remain in warm directory")
	}
}

// Test 3.2: Archive creates cold directory if it doesn't exist
func TestArchiveWarmToCold_CreatesColdDir(t *testing.T) {
	tempDir := t.TempDir()
	warmDir := filepath.Join(tempDir, "warm")
	coldDir := filepath.Join(tempDir, "cold", "nested", "path")

	if err := os.MkdirAll(warmDir, 0755); err != nil {
		t.Fatalf("Failed to create warm directory: %v", err)
	}

	os.WriteFile(filepath.Join(warmDir, "test_001.jsonl"), []byte("content"), 0644)

	archived, err := ArchiveWarmToCold(warmDir, coldDir)
	if err != nil {
		t.Fatalf("ArchiveWarmToCold failed: %v", err)
	}

	if archived != 1 {
		t.Errorf("Archived count: got %d, want 1", archived)
	}

	// Verify cold directory was created
	if !fileExists(coldDir) {
		t.Errorf("Cold directory should have been created")
	}

	// Verify file exists in cold
	if !fileExists(filepath.Join(coldDir, "test_001.jsonl.gz")) {
		t.Errorf("Archived file should exist in cold directory")
	}
}

// Helper functions

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func readGzipFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	gzReader, err := gzip.NewReader(file)
	if err != nil {
		return "", err
	}
	defer gzReader.Close()

	content, err := io.ReadAll(gzReader)
	if err != nil {
		return "", err
	}

	return string(content), nil
}
