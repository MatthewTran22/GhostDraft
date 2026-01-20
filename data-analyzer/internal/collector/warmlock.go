package collector

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// WarmLock is a thin wrapper around sync.RWMutex that provides:
// - Logging for lock acquisition/release
// - Metrics for lock contention time (wait time, hold time)
//
// Purpose: Prevent hot→warm rotation while reducer is processing warm files.
// - Collector rotation: RLock() - multiple rotations can happen simultaneously
// - Reducer processing: Lock() - exclusive, blocks all rotations
type WarmLock struct {
	mu sync.RWMutex

	// Metrics tracking
	metricsLock sync.Mutex

	// Exclusive lock metrics
	exclusiveLockCount      int64
	totalExclusiveWaitTime  time.Duration
	totalExclusiveHoldTime  time.Duration
	currentExclusiveLockAt  time.Time

	// Read lock metrics
	readLockCount          int64
	activeReadLocks        atomic.Int64
	readLockHoldStartTimes sync.Map // goroutine tracking for hold times
}

// WarmLockMetrics contains the metrics collected by the lock
type WarmLockMetrics struct {
	ExclusiveLockCount      int64
	TotalExclusiveWaitTime  time.Duration
	TotalExclusiveHoldTime  time.Duration
	ReadLockCount           int64
	CurrentActiveReadLocks  int64
}

// NewWarmLock creates a new WarmLock
func NewWarmLock() *WarmLock {
	return &WarmLock{}
}

// RLock acquires a read lock (used by collector for rotation)
// Multiple read locks can be held simultaneously
func (w *WarmLock) RLock() {
	start := time.Now()
	w.mu.RLock()
	waitTime := time.Since(start)

	w.metricsLock.Lock()
	w.readLockCount++
	w.metricsLock.Unlock()

	w.activeReadLocks.Add(1)

	// Store start time for this read lock (keyed by current time as unique ID)
	// In a real scenario, we'd use goroutine ID, but Go doesn't expose that
	// For metrics purposes, we track active count
	if waitTime > 10*time.Millisecond {
		fmt.Printf("[WarmLock] RLock acquired after waiting %v\n", waitTime)
	}
}

// RUnlock releases a read lock
func (w *WarmLock) RUnlock() {
	w.activeReadLocks.Add(-1)
	w.mu.RUnlock()
}

// Lock acquires an exclusive lock (used by reducer to process warm files)
// Blocks until all read locks are released
func (w *WarmLock) Lock() {
	start := time.Now()
	w.mu.Lock()
	waitTime := time.Since(start)

	w.metricsLock.Lock()
	w.exclusiveLockCount++
	w.totalExclusiveWaitTime += waitTime
	w.currentExclusiveLockAt = time.Now()
	w.metricsLock.Unlock()

	if waitTime > 10*time.Millisecond {
		fmt.Printf("[WarmLock] Exclusive lock acquired after waiting %v\n", waitTime)
	} else {
		fmt.Printf("[WarmLock] Exclusive lock acquired\n")
	}
}

// Unlock releases an exclusive lock
func (w *WarmLock) Unlock() {
	w.metricsLock.Lock()
	holdTime := time.Since(w.currentExclusiveLockAt)
	w.totalExclusiveHoldTime += holdTime
	w.metricsLock.Unlock()

	fmt.Printf("[WarmLock] Exclusive lock released (held for %v)\n", holdTime)
	w.mu.Unlock()
}

// Metrics returns current metrics for the lock
func (w *WarmLock) Metrics() WarmLockMetrics {
	w.metricsLock.Lock()
	defer w.metricsLock.Unlock()

	return WarmLockMetrics{
		ExclusiveLockCount:      w.exclusiveLockCount,
		TotalExclusiveWaitTime:  w.totalExclusiveWaitTime,
		TotalExclusiveHoldTime:  w.totalExclusiveHoldTime,
		ReadLockCount:           w.readLockCount,
		CurrentActiveReadLocks:  w.activeReadLocks.Load(),
	}
}

// ResetMetrics resets all metrics counters
func (w *WarmLock) ResetMetrics() {
	w.metricsLock.Lock()
	defer w.metricsLock.Unlock()

	w.exclusiveLockCount = 0
	w.totalExclusiveWaitTime = 0
	w.totalExclusiveHoldTime = 0
	w.readLockCount = 0
}
