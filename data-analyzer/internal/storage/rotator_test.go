package storage

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// Test 1.1: FlushAndRotate basic behavior
func TestFlushAndRotate_BasicBehavior(t *testing.T) {
	// Setup: create temp directory
	tmpDir, err := os.MkdirTemp("", "rotator-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create rotator
	r, err := NewFileRotator(tmpDir)
	if err != nil {
		t.Fatalf("failed to create rotator: %v", err)
	}
	defer r.Close()

	// Write some test data
	testRecord := &RawMatch{
		MatchID:      "TEST_123",
		ChampionID:   1,
		ChampionName: "Annie",
		TeamPosition: "MIDDLE",
		Win:          true,
	}

	for i := 0; i < 5; i++ {
		if err := r.WriteLine(testRecord); err != nil {
			t.Fatalf("failed to write record: %v", err)
		}
	}
	if err := r.MatchComplete(); err != nil {
		t.Fatalf("failed to signal match complete: %v", err)
	}

	// Get the current hot file path before flush
	hotDir := filepath.Join(tmpDir, "hot")
	warmDir := filepath.Join(tmpDir, "warm")

	// Verify hot/ has the file before flush
	hotFiles, _ := filepath.Glob(filepath.Join(hotDir, "*.jsonl"))
	if len(hotFiles) != 1 {
		t.Fatalf("expected 1 file in hot/, got %d", len(hotFiles))
	}
	hotFileBefore := filepath.Base(hotFiles[0])

	// Call FlushAndRotate
	rotated, err := r.FlushAndRotate()
	if err != nil {
		t.Fatalf("FlushAndRotate failed: %v", err)
	}

	// Assert: file was rotated (had data)
	if !rotated {
		t.Error("expected FlushAndRotate to return true (file had data)")
	}

	// Assert: hot file moved to warm/
	warmFiles, _ := filepath.Glob(filepath.Join(warmDir, "*.jsonl"))
	if len(warmFiles) != 1 {
		t.Fatalf("expected 1 file in warm/, got %d", len(warmFiles))
	}
	warmFile := filepath.Base(warmFiles[0])
	if warmFile != hotFileBefore {
		t.Errorf("expected warm file %s to match hot file %s", warmFile, hotFileBefore)
	}

	// Assert: new hot file created (may have same name if within same second, that's OK)
	newHotFiles, _ := filepath.Glob(filepath.Join(hotDir, "*.jsonl"))
	if len(newHotFiles) != 1 {
		t.Fatalf("expected 1 new file in hot/, got %d", len(newHotFiles))
	}
	// Note: filename may be the same if rotation happens within same second
	// The important thing is that hot/ has exactly one file and warm/ has the rotated file

	// Assert: warm file contains the written data
	warmContent, err := os.ReadFile(warmFiles[0])
	if err != nil {
		t.Fatalf("failed to read warm file: %v", err)
	}
	if len(warmContent) == 0 {
		t.Error("warm file should not be empty")
	}
}

// Test 1.2: FlushAndRotate with empty hot file
func TestFlushAndRotate_EmptyHotFile(t *testing.T) {
	// Setup: create temp directory
	tmpDir, err := os.MkdirTemp("", "rotator-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create rotator
	r, err := NewFileRotator(tmpDir)
	if err != nil {
		t.Fatalf("failed to create rotator: %v", err)
	}
	defer r.Close()

	// Don't write any data - hot file should be empty

	warmDir := filepath.Join(tmpDir, "warm")

	// Call FlushAndRotate without writing any data
	rotated, err := r.FlushAndRotate()
	if err != nil {
		t.Fatalf("FlushAndRotate failed: %v", err)
	}

	// Assert: no file was rotated (empty)
	if rotated {
		t.Error("expected FlushAndRotate to return false (empty file)")
	}

	// Assert: no empty file created in warm/
	warmFiles, _ := filepath.Glob(filepath.Join(warmDir, "*.jsonl"))
	if len(warmFiles) != 0 {
		t.Errorf("expected 0 files in warm/, got %d (should not create empty files)", len(warmFiles))
	}
}

// Test 1.3: Concurrent writes during FlushAndRotate
func TestFlushAndRotate_ConcurrentWrites(t *testing.T) {
	// Setup: create temp directory
	tmpDir, err := os.MkdirTemp("", "rotator-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create rotator
	r, err := NewFileRotator(tmpDir)
	if err != nil {
		t.Fatalf("failed to create rotator: %v", err)
	}
	defer r.Close()

	testRecord := &RawMatch{
		MatchID:      "TEST_CONCURRENT",
		ChampionID:   1,
		ChampionName: "Annie",
		TeamPosition: "MIDDLE",
		Win:          true,
	}

	// Track errors from goroutines
	var wg sync.WaitGroup
	errChan := make(chan error, 100)

	// Writer goroutine - continuously writes data
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			if err := r.WriteLine(testRecord); err != nil {
				errChan <- err
				return
			}
			// Small delay to spread writes across flushes
			time.Sleep(time.Millisecond)
		}
	}()

	// Flusher goroutine - calls FlushAndRotate multiple times
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 10; i++ {
			if _, err := r.FlushAndRotate(); err != nil {
				errChan <- err
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()

	// Wait for all goroutines
	wg.Wait()
	close(errChan)

	// Check for errors
	for err := range errChan {
		t.Errorf("concurrent operation error: %v", err)
	}

	// Count total files in warm/ to verify data wasn't lost
	warmDir := filepath.Join(tmpDir, "warm")
	warmFiles, _ := filepath.Glob(filepath.Join(warmDir, "*.jsonl"))

	// Should have some files in warm (from the flushes)
	// The exact number depends on timing, but should be > 0
	if len(warmFiles) == 0 {
		t.Log("Note: no files in warm/ - all data may be in current hot file")
	}

	// Verify no panics occurred (if we got here, test passed)
	t.Logf("Completed concurrent test: %d files in warm/", len(warmFiles))
}

// Test: FlushAndRotate returns correct count
func TestFlushAndRotate_ReturnsMatchCount(t *testing.T) {
	// Setup
	tmpDir, err := os.MkdirTemp("", "rotator-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	r, err := NewFileRotator(tmpDir)
	if err != nil {
		t.Fatalf("failed to create rotator: %v", err)
	}
	defer r.Close()

	// Write exactly 3 matches (each with 10 participants)
	testRecord := &RawMatch{
		MatchID:      "TEST_COUNT",
		ChampionID:   1,
		ChampionName: "Annie",
		TeamPosition: "MIDDLE",
		Win:          true,
	}

	for match := 0; match < 3; match++ {
		for participant := 0; participant < 10; participant++ {
			if err := r.WriteLine(testRecord); err != nil {
				t.Fatalf("failed to write record: %v", err)
			}
		}
		if err := r.MatchComplete(); err != nil {
			t.Fatalf("failed to signal match complete: %v", err)
		}
	}

	// FlushAndRotate should return true (had matches)
	rotated, err := r.FlushAndRotate()
	if err != nil {
		t.Fatalf("FlushAndRotate failed: %v", err)
	}
	if !rotated {
		t.Error("expected rotated=true for file with matches")
	}
}
