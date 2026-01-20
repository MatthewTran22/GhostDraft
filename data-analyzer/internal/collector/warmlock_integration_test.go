package collector

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"data-analyzer/internal/storage"
)

// 2.4 Integration: Lock with Rotator under load
// Spin up 10 goroutines continuously rotating files
// Periodically acquire exclusive Lock (simulating reducer)
// Assert no files lost, no race conditions
func TestWarmLock_Integration_WithRotatorUnderLoad(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "warmlock_integration_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create rotator
	rotator, err := storage.NewFileRotator(tmpDir)
	if err != nil {
		t.Fatalf("failed to create rotator: %v", err)
	}
	defer rotator.Close()

	// Create warm lock
	warmLock := NewWarmLock()

	// Track stats
	var totalRotations atomic.Int64
	var reducerCycles atomic.Int64
	var filesLost atomic.Int64
	var stopSignal atomic.Bool

	var wg sync.WaitGroup

	// Start 10 writer goroutines that continuously write and trigger rotations
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			type testRecord struct {
				WorkerID int `json:"workerId"`
				MatchNum int `json:"matchNum"`
			}

			matchNum := 0
			for !stopSignal.Load() {
				// Acquire RLock before rotation (simulating what collector would do)
				warmLock.RLock()

				// Write some data
				for j := 0; j < 10; j++ { // 10 participants per match
					if err := rotator.WriteLine(testRecord{WorkerID: workerID, MatchNum: matchNum}); err != nil {
						t.Errorf("worker %d: write failed: %v", workerID, err)
					}
				}

				// Complete the match (may trigger rotation)
				if err := rotator.MatchComplete(); err != nil {
					t.Errorf("worker %d: match complete failed: %v", workerID, err)
				}

				warmLock.RUnlock()

				matchNum++
				if matchNum%100 == 0 {
					totalRotations.Add(1)
				}

				// Small delay to not overwhelm the system
				time.Sleep(1 * time.Millisecond)
			}
		}(i)
	}

	// Start reducer simulation goroutine that periodically acquires exclusive lock
	wg.Add(1)
	go func() {
		defer wg.Done()

		for i := 0; i < 5; i++ { // 5 reducer cycles
			time.Sleep(100 * time.Millisecond)

			// Acquire exclusive lock (simulating reducer)
			warmLock.Lock()

			// Count files in warm directory
			warmDir := filepath.Join(tmpDir, "warm")
			files, err := os.ReadDir(warmDir)
			if err != nil && !os.IsNotExist(err) {
				t.Errorf("failed to read warm dir: %v", err)
			}

			// Simulate processing time
			time.Sleep(20 * time.Millisecond)

			// "Process" files (in real reducer, this would aggregate and archive)
			for _, f := range files {
				if f.IsDir() {
					continue
				}
				// Just verify file exists and can be accessed
				fpath := filepath.Join(warmDir, f.Name())
				info, err := os.Stat(fpath)
				if err != nil {
					filesLost.Add(1)
					t.Errorf("file lost during processing: %s: %v", f.Name(), err)
				}
				_ = info
			}

			warmLock.Unlock()
			reducerCycles.Add(1)
		}

		// Signal workers to stop
		stopSignal.Store(true)
	}()

	wg.Wait()

	// Verify results
	if filesLost.Load() > 0 {
		t.Errorf("files lost: %d", filesLost.Load())
	}

	if reducerCycles.Load() != 5 {
		t.Errorf("expected 5 reducer cycles, got %d", reducerCycles.Load())
	}

	// Check metrics
	metrics := warmLock.Metrics()
	t.Logf("Integration test metrics:")
	t.Logf("  Exclusive lock count: %d", metrics.ExclusiveLockCount)
	t.Logf("  Total exclusive wait time: %v", metrics.TotalExclusiveWaitTime)
	t.Logf("  Total exclusive hold time: %v", metrics.TotalExclusiveHoldTime)
	t.Logf("  Read lock count: %d", metrics.ReadLockCount)
}

// 2.5 Integration: Lock contention metrics
// Simulate high contention scenario
// Verify metrics captured (wait time, hold time)
func TestWarmLock_Integration_ContentionMetrics(t *testing.T) {
	warmLock := NewWarmLock()

	var wg sync.WaitGroup

	// Start exclusive lock holder that holds lock for extended periods
	wg.Add(1)
	go func() {
		defer wg.Done()

		for i := 0; i < 3; i++ {
			warmLock.Lock()
			// Simulate long processing time
			time.Sleep(100 * time.Millisecond)
			warmLock.Unlock()
			// Brief pause between locks
			time.Sleep(50 * time.Millisecond)
		}
	}()

	// Start 20 goroutines that try to acquire read locks
	var totalReadWaitTime atomic.Int64

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for j := 0; j < 5; j++ {
				start := time.Now()
				warmLock.RLock()
				waitTime := time.Since(start)
				totalReadWaitTime.Add(waitTime.Nanoseconds())

				// Brief hold
				time.Sleep(5 * time.Millisecond)
				warmLock.RUnlock()

				time.Sleep(10 * time.Millisecond)
			}
		}()
	}

	wg.Wait()

	// Verify metrics
	metrics := warmLock.Metrics()

	// Should have 3 exclusive locks
	if metrics.ExclusiveLockCount != 3 {
		t.Errorf("expected 3 exclusive locks, got %d", metrics.ExclusiveLockCount)
	}

	// Exclusive hold time should be at least 300ms (3 * 100ms)
	if metrics.TotalExclusiveHoldTime < 300*time.Millisecond {
		t.Errorf("expected exclusive hold time >= 300ms, got %v", metrics.TotalExclusiveHoldTime)
	}

	// Read lock count should be 100 (20 goroutines * 5 iterations)
	if metrics.ReadLockCount != 100 {
		t.Errorf("expected 100 read locks, got %d", metrics.ReadLockCount)
	}

	// There should be some wait time due to contention
	t.Logf("Contention metrics:")
	t.Logf("  Exclusive lock count: %d", metrics.ExclusiveLockCount)
	t.Logf("  Total exclusive wait time: %v", metrics.TotalExclusiveWaitTime)
	t.Logf("  Total exclusive hold time: %v", metrics.TotalExclusiveHoldTime)
	t.Logf("  Read lock count: %d", metrics.ReadLockCount)
	t.Logf("  Total read wait time (measured): %v", time.Duration(totalReadWaitTime.Load()))
}

// Test that the lock doesn't cause deadlocks under stress
func TestWarmLock_Integration_NoDeadlock(t *testing.T) {
	warmLock := NewWarmLock()

	done := make(chan struct{})
	timeout := time.After(5 * time.Second)

	go func() {
		var wg sync.WaitGroup

		// 50 readers
		for i := 0; i < 50; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < 100; j++ {
					warmLock.RLock()
					time.Sleep(time.Microsecond)
					warmLock.RUnlock()
				}
			}()
		}

		// 5 writers
		for i := 0; i < 5; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < 20; j++ {
					warmLock.Lock()
					time.Sleep(100 * time.Microsecond)
					warmLock.Unlock()
				}
			}()
		}

		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success - no deadlock
	case <-timeout:
		t.Fatal("test timed out - possible deadlock")
	}

	// Check final metrics
	metrics := warmLock.Metrics()
	t.Logf("Stress test completed:")
	t.Logf("  Exclusive locks: %d (expected 100)", metrics.ExclusiveLockCount)
	t.Logf("  Read locks: %d (expected 5000)", metrics.ReadLockCount)

	if metrics.ExclusiveLockCount != 100 {
		t.Errorf("expected 100 exclusive locks, got %d", metrics.ExclusiveLockCount)
	}
	if metrics.ReadLockCount != 5000 {
		t.Errorf("expected 5000 read locks, got %d", metrics.ReadLockCount)
	}
}
