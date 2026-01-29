package collector

import (
	"context"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestSetupSignalHandler tests that the signal handler context works
func TestSetupSignalHandler(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Signal tests not supported on Windows")
	}

	var shutdownCalled atomic.Bool

	ctx := SetupSignalHandler(func(ctx context.Context) {
		shutdownCalled.Store(true)
	})

	// Context should not be cancelled initially
	select {
	case <-ctx.Done():
		t.Error("Context should not be cancelled initially")
	default:
		// Good
	}

	// Send SIGINT to ourselves
	p, _ := os.FindProcess(os.Getpid())
	p.Signal(os.Interrupt)

	// Wait for context to be cancelled
	select {
	case <-ctx.Done():
		// Good
	case <-time.After(1 * time.Second):
		t.Error("Context should be cancelled after signal")
	}

	// Verify shutdown was called
	if !shutdownCalled.Load() {
		t.Error("Shutdown function should have been called")
	}
}

// TestSetupSignalHandler_NilShutdown tests that nil shutdown func doesn't panic
func TestSetupSignalHandler_NilShutdown(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Signal tests not supported on Windows")
	}

	ctx := SetupSignalHandler(nil)

	// Context should not be cancelled initially
	select {
	case <-ctx.Done():
		t.Error("Context should not be cancelled initially")
	default:
		// Good
	}

	// Send SIGINT to ourselves
	p, _ := os.FindProcess(os.Getpid())
	p.Signal(os.Interrupt)

	// Wait for context to be cancelled (should not panic)
	select {
	case <-ctx.Done():
		// Good
	case <-time.After(1 * time.Second):
		t.Error("Context should be cancelled after signal")
	}
}

// TestShutdownLogic tests the shutdown logic without signals
func TestShutdownLogic(t *testing.T) {
	var shutdownCalled atomic.Bool

	shutdownFunc := func(ctx context.Context) {
		shutdownCalled.Store(true)
	}

	// Create a context and manually trigger shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Call shutdown function directly
	shutdownFunc(ctx)

	if !shutdownCalled.Load() {
		t.Error("Shutdown function should have been called")
	}
}

// TestShutdown_Integration_FullSequence tests the full shutdown sequence
// 9.3 Integration: Full shutdown sequence
func TestShutdown_Integration_FullSequence(t *testing.T) {
	var reduceCallCount atomic.Int32
	var reduceDone atomic.Bool

	// Create mock components
	spider := &mockSpiderForTest{}
	reduceFunc := func(ctx context.Context) error {
		reduceCallCount.Add(1)
		time.Sleep(50 * time.Millisecond) // Simulate reduce work
		reduceDone.Store(true)
		return nil
	}
	keyValidator := &mockKeyValidatorForTest{valid: true}

	config := DefaultConfig()
	config.ShutdownTimeout = 5 * time.Second

	cc := NewContinuousCollector(spider, reduceFunc, keyValidator, nil, nil, config)

	// Start collector in background
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		_ = cc.Run(ctx)
		close(done)
	}()

	// Wait for collecting state
	time.Sleep(50 * time.Millisecond)
	if cc.State() != StateCollecting {
		t.Errorf("Expected StateCollecting, got %v", cc.State())
	}

	// Trigger shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	cc.Shutdown(shutdownCtx)
	cancel()

	// Wait for collector to finish
	select {
	case <-done:
		// Good
	case <-time.After(10 * time.Second):
		t.Fatal("Collector did not shut down in time")
	}

	// Verify final state is shutdown
	if cc.State() != StateShutdown {
		t.Errorf("Expected StateShutdown, got %v", cc.State())
	}

	// Reduce should have been triggered
	if reduceCallCount.Load() == 0 {
		t.Error("Reduce should have been called during shutdown")
	}
}

// TestShutdown_Integration_DuringActiveReduce tests shutdown during an active reduce
// 9.4 Integration: Shutdown during active reduce
func TestShutdown_Integration_DuringActiveReduce(t *testing.T) {
	reduceStarted := make(chan struct{})
	reduceComplete := make(chan struct{})
	var reduceCount atomic.Int32

	spider := &mockSpiderForTest{}
	reduceFunc := func(ctx context.Context) error {
		reduceCount.Add(1)
		close(reduceStarted)
		time.Sleep(200 * time.Millisecond) // Simulate slow reduce
		close(reduceComplete)
		return nil
	}
	keyValidator := &mockKeyValidatorForTest{valid: true}

	config := DefaultConfig()
	config.WarmFileThreshold = 1 // Trigger reduce immediately
	config.ShutdownTimeout = 5 * time.Second

	cc := NewContinuousCollector(spider, reduceFunc, keyValidator, nil, nil, config)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		cc.Run(ctx)
		close(done)
	}()

	// Trigger reduce by incrementing warm file counter
	time.Sleep(50 * time.Millisecond)
	cc.IncrementWarmFileCount()

	// Wait for reduce to start
	select {
	case <-reduceStarted:
		// Good
	case <-time.After(1 * time.Second):
		t.Fatal("Reduce did not start")
	}

	// Send shutdown while reduce is in progress
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	go cc.Shutdown(shutdownCtx)

	// Wait for reduce to complete (should not be interrupted)
	select {
	case <-reduceComplete:
		// Good - reduce completed
	case <-time.After(2 * time.Second):
		t.Fatal("Reduce should complete before shutdown")
	}

	cancel()

	// Wait for collector to finish
	select {
	case <-done:
		// Good
	case <-time.After(5 * time.Second):
		t.Fatal("Collector did not shut down after reduce")
	}

	// Only one reduce should have run (the one we triggered)
	if count := reduceCount.Load(); count != 1 {
		t.Errorf("Expected 1 reduce call, got %d", count)
	}
}

// TestShutdown_Integration_Timeout tests shutdown timeout
// 9.5 Integration: Shutdown timeout
func TestShutdown_Integration_Timeout(t *testing.T) {
	reduceBlocking := make(chan struct{})

	spider := &mockSpiderForTest{}
	reduceFunc := func(ctx context.Context) error {
		// This reduce blocks forever (simulating a stuck reduce)
		<-reduceBlocking
		return nil
	}
	keyValidator := &mockKeyValidatorForTest{valid: true}

	config := DefaultConfig()
	config.WarmFileThreshold = 1
	config.ShutdownTimeout = 200 * time.Millisecond // Short timeout

	cc := NewContinuousCollector(spider, reduceFunc, keyValidator, nil, nil, config)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		cc.Run(ctx)
		close(done)
	}()

	// Wait for collecting state and trigger reduce
	time.Sleep(50 * time.Millisecond)
	cc.IncrementWarmFileCount()

	// Give reduce time to start
	time.Sleep(50 * time.Millisecond)

	// Trigger shutdown with timeout
	start := time.Now()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer shutdownCancel()

	// Run shutdown in goroutine since it might block
	shutdownDone := make(chan struct{})
	go func() {
		cc.Shutdown(shutdownCtx)
		close(shutdownDone)
	}()

	// Shutdown should complete within timeout (not wait forever for reduce)
	select {
	case <-shutdownDone:
		elapsed := time.Since(start)
		if elapsed > 1*time.Second {
			t.Errorf("Shutdown took too long: %v", elapsed)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Shutdown should have timed out")
	}

	// Clean up
	close(reduceBlocking)
	cancel()

	select {
	case <-done:
	case <-time.After(1 * time.Second):
	}
}

// TestShutdown_Integration_MultipleSignals tests handling of multiple shutdown signals
// 9.6 Integration: Multiple signals
func TestShutdown_Integration_MultipleSignals(t *testing.T) {
	var shutdownCallCount atomic.Int32

	spider := &mockSpiderForTest{}
	reduceFunc := func(ctx context.Context) error {
		time.Sleep(50 * time.Millisecond)
		return nil
	}
	keyValidator := &mockKeyValidatorForTest{valid: true}

	config := DefaultConfig()
	config.ShutdownTimeout = 5 * time.Second

	cc := NewContinuousCollector(spider, reduceFunc, keyValidator, nil, nil, config)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})

	go func() {
		cc.Run(ctx)
		close(done)
	}()

	// Wait for collector to start
	time.Sleep(50 * time.Millisecond)

	// Call Shutdown multiple times concurrently
	var wg sync.WaitGroup
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cc.Shutdown(shutdownCtx)
			shutdownCallCount.Add(1)
		}()
	}

	// Wait for all shutdown calls to complete
	wg.Wait()

	// Cancel context to ensure collector exits
	cancel()

	// Wait for collector to finish
	select {
	case <-done:
		// Good
	case <-time.After(5 * time.Second):
		t.Fatal("Collector did not shut down")
	}

	// All shutdown calls should have completed without panicking
	if count := shutdownCallCount.Load(); count != 5 {
		t.Errorf("Expected 5 shutdown calls to complete, got %d", count)
	}

	// Final state should be shutdown
	if cc.State() != StateShutdown {
		t.Errorf("Expected StateShutdown, got %v", cc.State())
	}
}

// mockSpiderForTest is a test helper
type mockSpiderForTest struct {
	runCalls   atomic.Int32
	resetCalls atomic.Int32
	seedCalls  atomic.Int32
}

func (m *mockSpiderForTest) RunContinuous(ctx context.Context) error {
	m.runCalls.Add(1)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(10 * time.Millisecond):
		return nil
	}
}

func (m *mockSpiderForTest) Reset() {
	m.resetCalls.Add(1)
}

func (m *mockSpiderForTest) SeedFromChallenger(ctx context.Context) error {
	m.seedCalls.Add(1)
	return nil
}

// mockKeyValidatorForTest is a test helper
type mockKeyValidatorForTest struct {
	valid bool
}

func (m *mockKeyValidatorForTest) ValidateKey(key string) (bool, error) {
	return m.valid, nil
}
