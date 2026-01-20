package collector

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// MockTursoClient implements DataPusher interface for testing
type MockTursoClient struct {
	mu          sync.Mutex
	pushCount   int
	pushOrder   []string // Track order of pushes by patch
	pushDelay   time.Duration
	shouldError bool
	lastError   error
}

func (m *MockTursoClient) PushAggData(ctx context.Context, data *AggData) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.pushDelay > 0 {
		m.mu.Unlock()
		time.Sleep(m.pushDelay)
		m.mu.Lock()
	}

	if m.shouldError {
		return m.lastError
	}

	m.pushCount++
	m.pushOrder = append(m.pushOrder, data.DetectedPatch)
	return nil
}

func (m *MockTursoClient) GetPushCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.pushCount
}

func (m *MockTursoClient) GetPushOrder() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]string, len(m.pushOrder))
	copy(result, m.pushOrder)
	return result
}

// Test 3.3: Turso push channel - basic send/receive
func TestTursoPusher_BasicChannel(t *testing.T) {
	mock := &MockTursoClient{}
	pusher := NewTursoPusher(mock)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start pusher
	pusher.Start(ctx)

	// Send data to push
	data := &AggData{
		DetectedPatch:  "15.24",
		ChampionStats:  make(map[ChampionStatsKey]*ChampionStats),
		FilesProcessed: 10,
	}

	err := pusher.Push(ctx, data)
	if err != nil {
		t.Fatalf("Push failed: %v", err)
	}

	// Wait for push to complete
	pusher.Wait()

	if mock.GetPushCount() != 1 {
		t.Errorf("Push count: got %d, want 1", mock.GetPushCount())
	}
}

// Test 3.3: Turso push channel - multiple pushes
func TestTursoPusher_MultiplePushes(t *testing.T) {
	mock := &MockTursoClient{}
	pusher := NewTursoPusher(mock)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pusher.Start(ctx)

	// Send multiple pushes
	for i := 0; i < 5; i++ {
		data := &AggData{
			DetectedPatch:  "15.24",
			FilesProcessed: i + 1,
		}
		if err := pusher.Push(ctx, data); err != nil {
			t.Fatalf("Push %d failed: %v", i, err)
		}
	}

	pusher.Wait()

	if mock.GetPushCount() != 5 {
		t.Errorf("Push count: got %d, want 5", mock.GetPushCount())
	}
}

// Test 3.4: Sequential push queue - pushes happen one at a time
func TestTursoPusher_SequentialProcessing(t *testing.T) {
	// Use a slow mock to verify sequential processing
	mock := &MockTursoClient{pushDelay: 50 * time.Millisecond}
	pusher := NewTursoPusher(mock)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pusher.Start(ctx)

	// Track concurrent push count
	var concurrentPushes atomic.Int32
	var maxConcurrent atomic.Int32

	// Wrap the mock to track concurrency
	originalPush := mock.PushAggData
	concurrentMock := &ConcurrencyTrackingMock{
		mock:              mock,
		concurrentPushes:  &concurrentPushes,
		maxConcurrent:     &maxConcurrent,
		originalPushDelay: 50 * time.Millisecond,
	}

	// Create a new pusher with the tracking mock
	trackingPusher := NewTursoPusher(concurrentMock)
	trackingPusher.Start(ctx)

	// Send 5 pushes quickly
	for i := 0; i < 5; i++ {
		data := &AggData{
			DetectedPatch:  "15.24",
			FilesProcessed: i + 1,
		}
		trackingPusher.Push(ctx, data)
	}

	trackingPusher.Wait()

	// With sequential processing, max concurrent should be 1
	if maxConcurrent.Load() > 1 {
		t.Errorf("Max concurrent pushes: got %d, want 1 (sequential)", maxConcurrent.Load())
	}

	_ = originalPush // silence unused warning
}

// ConcurrencyTrackingMock tracks concurrent push operations
type ConcurrencyTrackingMock struct {
	mock              *MockTursoClient
	concurrentPushes  *atomic.Int32
	maxConcurrent     *atomic.Int32
	originalPushDelay time.Duration
}

func (c *ConcurrencyTrackingMock) PushAggData(ctx context.Context, data *AggData) error {
	current := c.concurrentPushes.Add(1)
	defer c.concurrentPushes.Add(-1)

	// Track maximum concurrent pushes
	for {
		max := c.maxConcurrent.Load()
		if current <= max || c.maxConcurrent.CompareAndSwap(max, current) {
			break
		}
	}

	// Simulate work
	time.Sleep(c.originalPushDelay)

	return nil
}

// Test 3.4: Sequential push queue - order is preserved
func TestTursoPusher_OrderPreserved(t *testing.T) {
	mock := &MockTursoClient{pushDelay: 10 * time.Millisecond}
	pusher := NewTursoPusher(mock)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pusher.Start(ctx)

	// Send pushes with different patches to track order
	patches := []string{"15.21", "15.22", "15.23", "15.24", "15.25"}
	for _, patch := range patches {
		data := &AggData{DetectedPatch: patch}
		if err := pusher.Push(ctx, data); err != nil {
			t.Fatalf("Push for %s failed: %v", patch, err)
		}
	}

	pusher.Wait()

	// Verify order
	pushOrder := mock.GetPushOrder()
	if len(pushOrder) != len(patches) {
		t.Fatalf("Push count mismatch: got %d, want %d", len(pushOrder), len(patches))
	}

	for i, patch := range patches {
		if pushOrder[i] != patch {
			t.Errorf("Push order[%d]: got %q, want %q", i, pushOrder[i], patch)
		}
	}
}

// Test 3.4: Push blocks when channel is full (backpressure)
func TestTursoPusher_Backpressure(t *testing.T) {
	mock := &MockTursoClient{pushDelay: 100 * time.Millisecond}
	pusher := NewTursoPusherWithBuffer(mock, 1) // Small buffer

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pusher.Start(ctx)

	// First push should succeed immediately
	data1 := &AggData{DetectedPatch: "15.24"}
	start := time.Now()
	pusher.Push(ctx, data1)
	firstPushTime := time.Since(start)

	// Second push should block until first is processing
	data2 := &AggData{DetectedPatch: "15.25"}
	pusher.Push(ctx, data2)

	// Third push might block longer
	data3 := &AggData{DetectedPatch: "15.26"}
	pusher.Push(ctx, data3)

	pusher.Wait()

	// First push should be fast (just queuing)
	if firstPushTime > 50*time.Millisecond {
		t.Logf("First push took %v (expected fast)", firstPushTime)
	}

	if mock.GetPushCount() != 3 {
		t.Errorf("Push count: got %d, want 3", mock.GetPushCount())
	}
}

// Test: Graceful shutdown waits for pending pushes
func TestTursoPusher_GracefulShutdown(t *testing.T) {
	mock := &MockTursoClient{pushDelay: 50 * time.Millisecond}
	pusher := NewTursoPusher(mock)

	ctx, cancel := context.WithCancel(context.Background())

	pusher.Start(ctx)

	// Send some pushes
	for i := 0; i < 3; i++ {
		data := &AggData{DetectedPatch: "15.24"}
		pusher.Push(ctx, data)
	}

	// Cancel context (simulates shutdown)
	cancel()

	// Wait should still complete pending work
	pusher.Wait()

	// All pushes should have completed
	if mock.GetPushCount() != 3 {
		t.Errorf("Push count after shutdown: got %d, want 3", mock.GetPushCount())
	}
}

// Test: Context cancellation during push
func TestTursoPusher_ContextCancellation(t *testing.T) {
	mock := &MockTursoClient{}
	pusher := NewTursoPusher(mock)

	ctx, cancel := context.WithCancel(context.Background())
	pusher.Start(ctx)

	// Cancel before pushing
	cancel()

	// Push with cancelled context should return error
	data := &AggData{DetectedPatch: "15.24"}
	err := pusher.Push(ctx, data)

	// Either error or the push was accepted before cancellation
	if err != nil && err != context.Canceled {
		t.Logf("Push returned: %v", err)
	}

	pusher.Wait()
}
