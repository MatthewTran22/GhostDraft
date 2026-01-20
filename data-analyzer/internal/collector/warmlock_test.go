package collector

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// 2.1 Unit: RLock allows concurrent rotations
func TestWarmLock_RLockAllowsConcurrent(t *testing.T) {
	lock := NewWarmLock()

	var count atomic.Int32
	var wg sync.WaitGroup

	// Start 10 goroutines that all acquire RLock
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			lock.RLock()
			count.Add(1)
			// Hold the lock briefly to ensure all goroutines overlap
			time.Sleep(50 * time.Millisecond)
			lock.RUnlock()
		}()
	}

	// Wait a bit for all goroutines to start and acquire locks
	time.Sleep(100 * time.Millisecond)

	// All 10 should have acquired the lock concurrently
	if count.Load() != 10 {
		t.Errorf("expected 10 concurrent RLocks, got %d", count.Load())
	}

	wg.Wait()
}

// 2.2 Unit: Lock blocks RLock
func TestWarmLock_LockBlocksRLock(t *testing.T) {
	lock := NewWarmLock()

	// Acquire exclusive lock
	lock.Lock()

	rlockAcquired := make(chan struct{})
	go func() {
		lock.RLock() // Should block
		close(rlockAcquired)
		lock.RUnlock()
	}()

	// Give the goroutine time to attempt RLock
	select {
	case <-rlockAcquired:
		t.Error("RLock should have been blocked by exclusive Lock")
	case <-time.After(100 * time.Millisecond):
		// Expected: RLock is blocked
	}

	// Release exclusive lock
	lock.Unlock()

	// Now RLock should succeed
	select {
	case <-rlockAcquired:
		// Expected: RLock acquired after Unlock
	case <-time.After(100 * time.Millisecond):
		t.Error("RLock should have succeeded after Unlock")
	}
}

// 2.3 Unit: RLock blocks Lock
func TestWarmLock_RLockBlocksLock(t *testing.T) {
	lock := NewWarmLock()

	// Acquire read lock
	lock.RLock()

	lockAcquired := make(chan struct{})
	go func() {
		lock.Lock() // Should block
		close(lockAcquired)
		lock.Unlock()
	}()

	// Give the goroutine time to attempt Lock
	select {
	case <-lockAcquired:
		t.Error("Lock should have been blocked by RLock")
	case <-time.After(100 * time.Millisecond):
		// Expected: Lock is blocked
	}

	// Release read lock
	lock.RUnlock()

	// Now Lock should succeed
	select {
	case <-lockAcquired:
		// Expected: Lock acquired after RUnlock
	case <-time.After(100 * time.Millisecond):
		t.Error("Lock should have succeeded after RUnlock")
	}
}

// Test metrics are captured correctly
func TestWarmLock_MetricsTracking(t *testing.T) {
	lock := NewWarmLock()

	// Acquire and hold exclusive lock briefly
	lock.Lock()
	time.Sleep(50 * time.Millisecond)
	lock.Unlock()

	// Get metrics
	metrics := lock.Metrics()

	if metrics.ExclusiveLockCount != 1 {
		t.Errorf("expected 1 exclusive lock, got %d", metrics.ExclusiveLockCount)
	}

	// Hold time should be at least 50ms
	if metrics.TotalExclusiveHoldTime < 50*time.Millisecond {
		t.Errorf("expected hold time >= 50ms, got %v", metrics.TotalExclusiveHoldTime)
	}
}

func TestWarmLock_ReadMetricsTracking(t *testing.T) {
	lock := NewWarmLock()

	// Acquire and hold read lock briefly
	lock.RLock()
	time.Sleep(50 * time.Millisecond)
	lock.RUnlock()

	// Get metrics
	metrics := lock.Metrics()

	if metrics.ReadLockCount != 1 {
		t.Errorf("expected 1 read lock, got %d", metrics.ReadLockCount)
	}
}

func TestWarmLock_WaitTimeMetrics(t *testing.T) {
	lock := NewWarmLock()

	// Acquire exclusive lock
	lock.Lock()

	waitStarted := make(chan struct{})
	waitDone := make(chan struct{})

	go func() {
		close(waitStarted)
		lock.Lock() // Will wait
		lock.Unlock()
		close(waitDone)
	}()

	<-waitStarted
	// Hold lock for 100ms to create measurable wait time
	time.Sleep(100 * time.Millisecond)
	lock.Unlock()

	<-waitDone

	metrics := lock.Metrics()

	// Second lock should have waited at least 100ms
	if metrics.TotalExclusiveWaitTime < 100*time.Millisecond {
		t.Errorf("expected wait time >= 100ms, got %v", metrics.TotalExclusiveWaitTime)
	}
}
