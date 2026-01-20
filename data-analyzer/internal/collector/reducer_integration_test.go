package collector

import (
	"compress/gzip"
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// =============================================================================
// Test 3.5: Full reduce cycle with real files
// =============================================================================

func TestReduceCycle_FullIntegration(t *testing.T) {
	tempDir := t.TempDir()
	warmDir := filepath.Join(tempDir, "warm")
	coldDir := filepath.Join(tempDir, "cold")

	if err := os.MkdirAll(warmDir, 0755); err != nil {
		t.Fatalf("Failed to create warm directory: %v", err)
	}

	// Create 10 realistic JSONL files with ~100 matches each (1000 records per file)
	totalMatches := 0
	for fileNum := 0; fileNum < 10; fileNum++ {
		var content string
		for matchNum := 0; matchNum < 100; matchNum++ {
			matchID := fmt.Sprintf("NA1_%d%03d", fileNum, matchNum)
			// 10 participants per match (5v5)
			for p := 0; p < 10; p++ {
				win := p < 5 // First 5 win, last 5 lose
				position := []string{"TOP", "JUNGLE", "MIDDLE", "BOTTOM", "UTILITY"}[p%5]
				championID := 100 + (p % 50) // Vary champions
				itemID := 3000 + (p % 20)    // Vary items

				content += fmt.Sprintf(
					`{"matchId":"%s","gameVersion":"15.24.1","gameDuration":1800,"gameCreation":1700000000000,"puuid":"p%d","championId":%d,"championName":"Champ%d","teamPosition":"%s","win":%t,"item0":%d,"item1":%d,"item2":0,"item3":0,"item4":0,"item5":0}`,
					matchID, p, championID, championID, position, win, itemID, itemID+100,
				) + "\n"
			}
			totalMatches++
		}

		filePath := filepath.Join(warmDir, fmt.Sprintf("matches_%03d.jsonl", fileNum))
		if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to write file %d: %v", fileNum, err)
		}
	}

	t.Logf("Created %d matches across 10 files", totalMatches)

	// Step 1: Aggregate
	itemFilter := func(itemID int) bool { return itemID >= 3000 }
	agg, err := AggregateWarmFiles(warmDir, itemFilter)
	if err != nil {
		t.Fatalf("AggregateWarmFiles failed: %v", err)
	}

	t.Logf("Aggregated: %d champion stats, %d item stats, %d matchup stats",
		len(agg.ChampionStats), len(agg.ItemStats), len(agg.MatchupStats))

	if agg.FilesProcessed != 10 {
		t.Errorf("FilesProcessed: got %d, want 10", agg.FilesProcessed)
	}

	// Verify we have stats
	if len(agg.ChampionStats) == 0 {
		t.Error("Expected champion stats to be populated")
	}
	if len(agg.ItemStats) == 0 {
		t.Error("Expected item stats to be populated")
	}
	if len(agg.MatchupStats) == 0 {
		t.Error("Expected matchup stats to be populated")
	}

	// Step 2: Archive
	archived, err := ArchiveWarmToCold(warmDir, coldDir)
	if err != nil {
		t.Fatalf("ArchiveWarmToCold failed: %v", err)
	}

	if archived != 10 {
		t.Errorf("Archived: got %d, want 10", archived)
	}

	// Step 3: Verify warm is empty
	warmFiles, _ := filepath.Glob(filepath.Join(warmDir, "*.jsonl"))
	if len(warmFiles) != 0 {
		t.Errorf("Warm directory should be empty, found %d files", len(warmFiles))
	}

	// Step 4: Verify cold has all files
	coldFiles, _ := filepath.Glob(filepath.Join(coldDir, "*.jsonl.gz"))
	if len(coldFiles) != 10 {
		t.Errorf("Cold directory should have 10 files, found %d", len(coldFiles))
	}

	// Step 5: Verify data integrity by decompressing and counting records
	totalRecords := 0
	for _, coldFile := range coldFiles {
		content, err := readGzipFileIntegration(coldFile)
		if err != nil {
			t.Errorf("Failed to read %s: %v", filepath.Base(coldFile), err)
			continue
		}
		// Count lines (each line is a record)
		for _, c := range content {
			if c == '\n' {
				totalRecords++
			}
		}
	}

	expectedRecords := totalMatches * 10 // 10 participants per match
	if totalRecords != expectedRecords {
		t.Errorf("Total records in cold: got %d, want %d", totalRecords, expectedRecords)
	}

	t.Logf("Full cycle complete: %d records archived to %d compressed files", totalRecords, len(coldFiles))
}

func readGzipFileIntegration(path string) (string, error) {
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

// =============================================================================
// Test 3.6: Reducer with WarmLock
// =============================================================================

func TestReduceCycle_WithWarmLock(t *testing.T) {
	tempDir := t.TempDir()
	warmDir := filepath.Join(tempDir, "warm")
	coldDir := filepath.Join(tempDir, "cold")

	if err := os.MkdirAll(warmDir, 0755); err != nil {
		t.Fatalf("Failed to create warm directory: %v", err)
	}

	// Create initial files
	for i := 0; i < 3; i++ {
		content := fmt.Sprintf(`{"matchId":"NA1_%d","gameVersion":"15.24.1","gameDuration":1800,"gameCreation":1700000000000,"puuid":"p1","championId":103,"championName":"Ahri","teamPosition":"MIDDLE","win":true,"item0":3089,"item1":0,"item2":0,"item3":0,"item4":0,"item5":0}
`, i)
		filePath := filepath.Join(warmDir, fmt.Sprintf("batch1_%03d.jsonl", i))
		os.WriteFile(filePath, []byte(content), 0644)
	}

	warmLock := NewWarmLock()
	var wg sync.WaitGroup
	var filesAddedDuringReduce atomic.Int32

	// Simulate reducer holding exclusive lock
	wg.Add(1)
	go func() {
		defer wg.Done()

		// Acquire exclusive lock (simulating reducer)
		warmLock.Lock()
		defer warmLock.Unlock()

		// Hold lock for a bit while "reducing"
		time.Sleep(100 * time.Millisecond)

		// Count files in warm (should be the original 3)
		files, _ := filepath.Glob(filepath.Join(warmDir, "*.jsonl"))
		if len(files) != 3 {
			t.Errorf("During reduce, expected 3 files, got %d", len(files))
		}
	}()

	// Simulate collector trying to add files while reducer runs
	wg.Add(1)
	go func() {
		defer wg.Done()

		// Wait a bit for reducer to start
		time.Sleep(20 * time.Millisecond)

		// Try to acquire RLock (simulating rotation)
		warmLock.RLock()
		defer warmLock.RUnlock()

		// Add a new file (this should happen AFTER reducer releases lock)
		content := `{"matchId":"NA1_new","gameVersion":"15.24.1","gameDuration":1800,"gameCreation":1700000000000,"puuid":"p1","championId":103,"championName":"Ahri","teamPosition":"MIDDLE","win":true,"item0":3089,"item1":0,"item2":0,"item3":0,"item4":0,"item5":0}
`
		filePath := filepath.Join(warmDir, "batch2_000.jsonl")
		os.WriteFile(filePath, []byte(content), 0644)
		filesAddedDuringReduce.Add(1)
	}()

	wg.Wait()

	// After both complete, warm should have 4 files (3 original + 1 new)
	files, _ := filepath.Glob(filepath.Join(warmDir, "*.jsonl"))
	if len(files) != 4 {
		t.Errorf("After reduce cycle, expected 4 files, got %d", len(files))
	}

	// The new file was added after reducer released lock
	if filesAddedDuringReduce.Load() != 1 {
		t.Error("Expected file to be added after reduce completed")
	}

	_ = coldDir // unused in this test
}

// =============================================================================
// Test 3.7: Turso push with in-memory SQLite
// =============================================================================

// InMemoryPusher implements DataPusher using in-memory SQLite
type InMemoryPusher struct {
	db *sql.DB
}

func NewInMemoryPusher() (*InMemoryPusher, error) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		return nil, err
	}

	// Create tables
	queries := []string{
		`CREATE TABLE champion_stats (
			patch TEXT NOT NULL,
			champion_id INTEGER NOT NULL,
			team_position TEXT NOT NULL,
			wins INTEGER NOT NULL DEFAULT 0,
			matches INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (patch, champion_id, team_position)
		)`,
		`CREATE TABLE champion_items (
			patch TEXT NOT NULL,
			champion_id INTEGER NOT NULL,
			team_position TEXT NOT NULL,
			item_id INTEGER NOT NULL,
			wins INTEGER NOT NULL DEFAULT 0,
			matches INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (patch, champion_id, team_position, item_id)
		)`,
		`CREATE TABLE champion_matchups (
			patch TEXT NOT NULL,
			champion_id INTEGER NOT NULL,
			team_position TEXT NOT NULL,
			enemy_champion_id INTEGER NOT NULL,
			wins INTEGER NOT NULL DEFAULT 0,
			matches INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (patch, champion_id, team_position, enemy_champion_id)
		)`,
	}

	for _, q := range queries {
		if _, err := db.Exec(q); err != nil {
			db.Close()
			return nil, err
		}
	}

	return &InMemoryPusher{db: db}, nil
}

func (p *InMemoryPusher) Close() error {
	return p.db.Close()
}

func (p *InMemoryPusher) PushAggData(ctx context.Context, data *AggData) error {
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Insert champion stats
	for k, v := range data.ChampionStats {
		_, err := tx.ExecContext(ctx,
			`INSERT INTO champion_stats (patch, champion_id, team_position, wins, matches)
			 VALUES (?, ?, ?, ?, ?)
			 ON CONFLICT(patch, champion_id, team_position) DO UPDATE SET
			 wins = wins + excluded.wins, matches = matches + excluded.matches`,
			k.Patch, k.ChampionID, k.TeamPosition, v.Wins, v.Matches)
		if err != nil {
			return err
		}
	}

	// Insert item stats
	for k, v := range data.ItemStats {
		_, err := tx.ExecContext(ctx,
			`INSERT INTO champion_items (patch, champion_id, team_position, item_id, wins, matches)
			 VALUES (?, ?, ?, ?, ?, ?)
			 ON CONFLICT(patch, champion_id, team_position, item_id) DO UPDATE SET
			 wins = wins + excluded.wins, matches = matches + excluded.matches`,
			k.Patch, k.ChampionID, k.TeamPosition, k.ItemID, v.Wins, v.Matches)
		if err != nil {
			return err
		}
	}

	// Insert matchup stats
	for k, v := range data.MatchupStats {
		_, err := tx.ExecContext(ctx,
			`INSERT INTO champion_matchups (patch, champion_id, team_position, enemy_champion_id, wins, matches)
			 VALUES (?, ?, ?, ?, ?, ?)
			 ON CONFLICT(patch, champion_id, team_position, enemy_champion_id) DO UPDATE SET
			 wins = wins + excluded.wins, matches = matches + excluded.matches`,
			k.Patch, k.ChampionID, k.TeamPosition, k.EnemyChampionID, v.Wins, v.Matches)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (p *InMemoryPusher) GetChampionStats(patch string, championID int, position string) (wins, matches int, err error) {
	err = p.db.QueryRow(
		`SELECT wins, matches FROM champion_stats WHERE patch = ? AND champion_id = ? AND team_position = ?`,
		patch, championID, position,
	).Scan(&wins, &matches)
	return
}

func (p *InMemoryPusher) CountRows(table string) (int, error) {
	var count int
	err := p.db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s", table)).Scan(&count)
	return count, err
}

func TestTursoPush_InMemory_BasicInsert(t *testing.T) {
	pusher, err := NewInMemoryPusher()
	if err != nil {
		t.Fatalf("Failed to create in-memory pusher: %v", err)
	}
	defer pusher.Close()

	// Create test data
	data := &AggData{
		DetectedPatch: "15.24",
		ChampionStats: map[ChampionStatsKey]*ChampionStats{
			{Patch: "15.24", ChampionID: 103, TeamPosition: "MIDDLE"}: {Wins: 10, Matches: 20},
			{Patch: "15.24", ChampionID: 238, TeamPosition: "MIDDLE"}: {Wins: 8, Matches: 15},
		},
		ItemStats: map[ItemStatsKey]*ItemStats{
			{Patch: "15.24", ChampionID: 103, TeamPosition: "MIDDLE", ItemID: 3089}: {Wins: 7, Matches: 12},
		},
		MatchupStats: map[MatchupStatsKey]*MatchupStats{
			{Patch: "15.24", ChampionID: 103, TeamPosition: "MIDDLE", EnemyChampionID: 238}: {Wins: 6, Matches: 10},
		},
	}

	// Push data
	ctx := context.Background()
	if err := pusher.PushAggData(ctx, data); err != nil {
		t.Fatalf("PushAggData failed: %v", err)
	}

	// Verify champion stats
	wins, matches, err := pusher.GetChampionStats("15.24", 103, "MIDDLE")
	if err != nil {
		t.Fatalf("GetChampionStats failed: %v", err)
	}
	if wins != 10 || matches != 20 {
		t.Errorf("Ahri stats: got wins=%d matches=%d, want wins=10 matches=20", wins, matches)
	}

	// Verify row counts
	champCount, _ := pusher.CountRows("champion_stats")
	if champCount != 2 {
		t.Errorf("Champion stats rows: got %d, want 2", champCount)
	}

	itemCount, _ := pusher.CountRows("champion_items")
	if itemCount != 1 {
		t.Errorf("Item stats rows: got %d, want 1", itemCount)
	}

	matchupCount, _ := pusher.CountRows("champion_matchups")
	if matchupCount != 1 {
		t.Errorf("Matchup stats rows: got %d, want 1", matchupCount)
	}
}

func TestTursoPush_InMemory_UpsertBehavior(t *testing.T) {
	pusher, err := NewInMemoryPusher()
	if err != nil {
		t.Fatalf("Failed to create in-memory pusher: %v", err)
	}
	defer pusher.Close()

	ctx := context.Background()

	// First push
	data1 := &AggData{
		DetectedPatch: "15.24",
		ChampionStats: map[ChampionStatsKey]*ChampionStats{
			{Patch: "15.24", ChampionID: 103, TeamPosition: "MIDDLE"}: {Wins: 10, Matches: 20},
		},
	}
	if err := pusher.PushAggData(ctx, data1); err != nil {
		t.Fatalf("First push failed: %v", err)
	}

	// Second push with overlapping data (same key)
	data2 := &AggData{
		DetectedPatch: "15.24",
		ChampionStats: map[ChampionStatsKey]*ChampionStats{
			{Patch: "15.24", ChampionID: 103, TeamPosition: "MIDDLE"}: {Wins: 5, Matches: 10},
		},
	}
	if err := pusher.PushAggData(ctx, data2); err != nil {
		t.Fatalf("Second push failed: %v", err)
	}

	// Verify upsert accumulated the values
	wins, matches, err := pusher.GetChampionStats("15.24", 103, "MIDDLE")
	if err != nil {
		t.Fatalf("GetChampionStats failed: %v", err)
	}

	// Should be 10+5=15 wins, 20+10=30 matches
	if wins != 15 || matches != 30 {
		t.Errorf("After upsert: got wins=%d matches=%d, want wins=15 matches=30", wins, matches)
	}

	// Should still be only 1 row
	count, _ := pusher.CountRows("champion_stats")
	if count != 1 {
		t.Errorf("Row count after upsert: got %d, want 1", count)
	}
}

// =============================================================================
// Test 3.8: Async push doesn't block reducer
// =============================================================================

func TestAsyncPush_DoesNotBlockReducer(t *testing.T) {
	tempDir := t.TempDir()
	warmDir := filepath.Join(tempDir, "warm")
	coldDir := filepath.Join(tempDir, "cold")

	os.MkdirAll(warmDir, 0755)

	// Create a file to process
	content := `{"matchId":"NA1_1","gameVersion":"15.24.1","gameDuration":1800,"gameCreation":1700000000000,"puuid":"p1","championId":103,"championName":"Ahri","teamPosition":"MIDDLE","win":true,"item0":3089,"item1":0,"item2":0,"item3":0,"item4":0,"item5":0}
{"matchId":"NA1_1","gameVersion":"15.24.1","gameDuration":1800,"gameCreation":1700000000000,"puuid":"p2","championId":238,"championName":"Zed","teamPosition":"MIDDLE","win":false,"item0":3142,"item1":0,"item2":0,"item3":0,"item4":0,"item5":0}
`
	os.WriteFile(filepath.Join(warmDir, "test.jsonl"), []byte(content), 0644)

	// Create a slow mock pusher
	slowPusher := &SlowMockPusher{delay: 200 * time.Millisecond}
	tursoPusher := NewTursoPusher(slowPusher)

	ctx := context.Background()
	tursoPusher.Start(ctx)

	// Measure time for aggregate + archive + queue push
	start := time.Now()

	// Step 1: Aggregate (fast)
	itemFilter := func(itemID int) bool { return itemID >= 3000 }
	agg, err := AggregateWarmFiles(warmDir, itemFilter)
	if err != nil {
		t.Fatalf("Aggregate failed: %v", err)
	}

	// Step 2: Archive (fast)
	_, err = ArchiveWarmToCold(warmDir, coldDir)
	if err != nil {
		t.Fatalf("Archive failed: %v", err)
	}

	// Step 3: Queue push (should return immediately, not wait for slow push)
	err = tursoPusher.Push(ctx, agg)
	if err != nil {
		t.Fatalf("Push queue failed: %v", err)
	}

	// This should complete quickly (< 100ms) even though push takes 200ms
	elapsed := time.Since(start)

	if elapsed > 100*time.Millisecond {
		t.Errorf("Reduce cycle took %v, expected < 100ms (push should be async)", elapsed)
	}

	t.Logf("Reduce cycle completed in %v (push is async)", elapsed)

	// Verify push eventually completes
	tursoPusher.Wait()

	if slowPusher.pushCount != 1 {
		t.Errorf("Push count: got %d, want 1", slowPusher.pushCount)
	}

	totalTime := time.Since(start)
	if totalTime < 200*time.Millisecond {
		t.Errorf("Total time %v should be >= 200ms (slow push)", totalTime)
	}

	t.Logf("Total time including async push: %v", totalTime)
}

type SlowMockPusher struct {
	delay     time.Duration
	pushCount int
	mu        sync.Mutex
}

func (s *SlowMockPusher) PushAggData(ctx context.Context, data *AggData) error {
	time.Sleep(s.delay)
	s.mu.Lock()
	s.pushCount++
	s.mu.Unlock()
	return nil
}

// =============================================================================
// Test: Full pipeline integration (aggregate -> archive -> async push)
// =============================================================================

func TestFullPipeline_Integration(t *testing.T) {
	tempDir := t.TempDir()
	warmDir := filepath.Join(tempDir, "warm")
	coldDir := filepath.Join(tempDir, "cold")

	os.MkdirAll(warmDir, 0755)

	// Create realistic test data
	for i := 0; i < 5; i++ {
		var content string
		for j := 0; j < 10; j++ {
			matchID := fmt.Sprintf("NA1_%d%d", i, j)
			content += fmt.Sprintf(
				`{"matchId":"%s","gameVersion":"15.24.1","gameDuration":1800,"gameCreation":1700000000000,"puuid":"p1","championId":103,"championName":"Ahri","teamPosition":"MIDDLE","win":true,"item0":3089,"item1":3157,"item2":0,"item3":0,"item4":0,"item5":0}
{"matchId":"%s","gameVersion":"15.24.1","gameDuration":1800,"gameCreation":1700000000000,"puuid":"p2","championId":238,"championName":"Zed","teamPosition":"MIDDLE","win":false,"item0":3142,"item1":3071,"item2":0,"item3":0,"item4":0,"item5":0}
`, matchID, matchID)
		}
		os.WriteFile(filepath.Join(warmDir, fmt.Sprintf("file_%d.jsonl", i)), []byte(content), 0644)
	}

	// Set up in-memory database
	pusher, err := NewInMemoryPusher()
	if err != nil {
		t.Fatalf("Failed to create pusher: %v", err)
	}
	defer pusher.Close()

	// Set up async pusher
	asyncPusher := NewTursoPusher(pusher)
	ctx := context.Background()
	asyncPusher.Start(ctx)

	// Run full pipeline
	itemFilter := func(itemID int) bool { return itemID >= 3000 }

	// 1. Aggregate
	agg, err := AggregateWarmFiles(warmDir, itemFilter)
	if err != nil {
		t.Fatalf("Aggregate failed: %v", err)
	}

	t.Logf("Aggregated: %d champion stats, %d item stats, %d matchups",
		len(agg.ChampionStats), len(agg.ItemStats), len(agg.MatchupStats))

	// 2. Archive
	archived, err := ArchiveWarmToCold(warmDir, coldDir)
	if err != nil {
		t.Fatalf("Archive failed: %v", err)
	}

	t.Logf("Archived %d files", archived)

	// 3. Queue push (async)
	err = asyncPusher.Push(ctx, agg)
	if err != nil {
		t.Fatalf("Push failed: %v", err)
	}

	// 4. Wait for push to complete
	asyncPusher.Wait()

	// Verify results
	// Ahri MIDDLE: 50 matches (10 per file * 5 files), 50 wins
	wins, matches, err := pusher.GetChampionStats("15.24", 103, "MIDDLE")
	if err != nil {
		t.Fatalf("GetChampionStats failed: %v", err)
	}

	if matches != 50 {
		t.Errorf("Ahri matches: got %d, want 50", matches)
	}
	if wins != 50 {
		t.Errorf("Ahri wins: got %d, want 50", wins)
	}

	// Zed MIDDLE: 50 matches, 0 wins
	wins, matches, err = pusher.GetChampionStats("15.24", 238, "MIDDLE")
	if err != nil {
		t.Fatalf("GetChampionStats failed: %v", err)
	}

	if matches != 50 {
		t.Errorf("Zed matches: got %d, want 50", matches)
	}
	if wins != 0 {
		t.Errorf("Zed wins: got %d, want 0", wins)
	}

	// Verify cold directory
	coldFiles, _ := filepath.Glob(filepath.Join(coldDir, "*.jsonl.gz"))
	if len(coldFiles) != 5 {
		t.Errorf("Cold files: got %d, want 5", len(coldFiles))
	}

	// Verify warm is empty
	warmFiles, _ := filepath.Glob(filepath.Join(warmDir, "*.jsonl"))
	if len(warmFiles) != 0 {
		t.Errorf("Warm should be empty, got %d files", len(warmFiles))
	}

	t.Log("Full pipeline integration test passed")
}
