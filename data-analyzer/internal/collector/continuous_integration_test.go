package collector

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestContinuousCollector_Integration_FullStateCycle tests the full state cycle
// STARTUP → COLLECTING → REDUCING → PUSHING → COLLECTING
func TestContinuousCollector_Integration_FullStateCycle(t *testing.T) {
	sm := NewStateMachine()
	warmLock := NewWarmLock()

	// Track state transitions
	var transitions []State
	var transitionMu sync.Mutex

	sm.OnTransition(func(from, to State) {
		transitionMu.Lock()
		transitions = append(transitions, to)
		transitionMu.Unlock()
	})

	// Create temp directory for warm files
	tempDir, err := os.MkdirTemp("", "continuous-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	warmDir := filepath.Join(tempDir, "warm")
	if err := os.MkdirAll(warmDir, 0755); err != nil {
		t.Fatalf("failed to create warm dir: %v", err)
	}

	// Simulate reduce operation
	reduceFunc := func() error {
		warmLock.Lock()
		defer warmLock.Unlock()

		// Simulate reading warm files
		time.Sleep(10 * time.Millisecond)
		return nil
	}

	// Full cycle
	// 1. STARTUP → COLLECTING
	if err := sm.TransitionTo(StateCollecting); err != nil {
		t.Fatalf("transition to COLLECTING: %v", err)
	}

	// 2. COLLECTING → REDUCING (triggered by warm file count)
	if !sm.TryTransitionToReducing() {
		t.Fatal("failed to transition to REDUCING")
	}

	// 3. Execute reduce
	if err := reduceFunc(); err != nil {
		t.Fatalf("reduce failed: %v", err)
	}

	// 4. REDUCING → PUSHING
	if err := sm.TransitionTo(StatePushing); err != nil {
		t.Fatalf("transition to PUSHING: %v", err)
	}

	// 5. PUSHING → COLLECTING
	if err := sm.TransitionTo(StateCollecting); err != nil {
		t.Fatalf("transition back to COLLECTING: %v", err)
	}

	// Verify transitions
	expected := []State{StateCollecting, StateReducing, StatePushing, StateCollecting}
	transitionMu.Lock()
	defer transitionMu.Unlock()

	if len(transitions) != len(expected) {
		t.Fatalf("got %d transitions, want %d", len(transitions), len(expected))
	}

	for i, exp := range expected {
		if transitions[i] != exp {
			t.Errorf("transition[%d] = %v, want %v", i, transitions[i], exp)
		}
	}
}

// TestContinuousCollector_Integration_KeyExpirationCycle tests the full key expiration cycle
func TestContinuousCollector_Integration_KeyExpirationCycle(t *testing.T) {
	sm := NewStateMachine()
	keyProvider := NewMockKeyProvider()
	keyValidator := &MockKeyValidator{isValid: true}

	// Track all state transitions
	var transitions []State
	var transitionMu sync.Mutex

	sm.OnTransition(func(from, to State) {
		transitionMu.Lock()
		transitions = append(transitions, to)
		transitionMu.Unlock()
	})

	// Start in COLLECTING
	sm.TransitionTo(StateCollecting)

	// Simulate API key expiration
	// 1. Error occurs, trigger reduce
	sm.TransitionTo(StateReducing)

	// 2. Reduce complete, push to Turso
	sm.TransitionTo(StatePushing)

	// 3. Key expired flag set, transition to WAITING_FOR_KEY
	sm.TransitionTo(StateWaitingForKey)

	// 4. Start key waiting loop in goroutine
	keyReceived := make(chan struct{})
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		newKey, err := keyProvider.WaitForKey(ctx)
		if err != nil {
			return
		}

		if keyValidator.ValidateKey(newKey) {
			sm.TransitionTo(StateFreshRestart)
			close(keyReceived)
		}
	}()

	// 5. Provide new key after short delay
	time.Sleep(50 * time.Millisecond)
	keyProvider.ProvideKey("RGAPI-new-key")

	// Wait for key to be processed
	select {
	case <-keyReceived:
		// Success
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for key to be processed")
	}

	// 6. FRESH_RESTART → STARTUP
	if err := sm.TransitionTo(StateStartup); err != nil {
		t.Fatalf("transition to STARTUP: %v", err)
	}

	// 7. STARTUP → COLLECTING (resume)
	if err := sm.TransitionTo(StateCollecting); err != nil {
		t.Fatalf("transition to COLLECTING: %v", err)
	}

	// Verify full cycle
	expected := []State{
		StateCollecting,    // Initial
		StateReducing,      // API error
		StatePushing,       // After reduce
		StateWaitingForKey, // Key expired
		StateFreshRestart,  // New key received
		StateStartup,       // Fresh start
		StateCollecting,    // Resume
	}

	transitionMu.Lock()
	defer transitionMu.Unlock()

	if len(transitions) != len(expected) {
		t.Fatalf("got %d transitions, want %d: %v", len(transitions), len(expected), transitions)
	}

	for i, exp := range expected {
		if transitions[i] != exp {
			t.Errorf("transition[%d] = %v, want %v", i, transitions[i], exp)
		}
	}
}

// TestContinuousCollector_Integration_DoubleTriggerPrevention tests that
// concurrent reduce triggers are properly serialized
func TestContinuousCollector_Integration_DoubleTriggerPrevention(t *testing.T) {
	sm := NewStateMachine()
	sm.TransitionTo(StateCollecting)

	var reduceCount atomic.Int32

	// Create warm file counter
	counter := NewWarmFileCounter(10, func() {
		if sm.TryTransitionToReducing() {
			reduceCount.Add(1)
		}
	})

	// Simulate concurrent triggers from different sources:
	// 1. Warm file count reaching threshold
	// 2. API error (401/403)

	var wg sync.WaitGroup
	wg.Add(2)

	// Source 1: Warm file counter threshold
	go func() {
		defer wg.Done()
		for i := 0; i < 10; i++ {
			counter.Increment()
		}
	}()

	// Source 2: API error trigger (simulated)
	go func() {
		defer wg.Done()
		// Small delay to create race condition
		time.Sleep(time.Millisecond)
		if sm.TryTransitionToReducing() {
			reduceCount.Add(1)
		}
	}()

	wg.Wait()

	// Only one reduce should have succeeded
	if reduceCount.Load() != 1 {
		t.Errorf("reduce count = %d, want 1", reduceCount.Load())
	}

	if sm.Current() != StateReducing {
		t.Errorf("state = %v, want %v", sm.Current(), StateReducing)
	}
}

// TestContinuousCollector_Integration_StatePersistenceAcrossComponents tests that
// different components respect the state machine's state
func TestContinuousCollector_Integration_StatePersistenceAcrossComponents(t *testing.T) {
	sm := NewStateMachine()
	warmLock := NewWarmLock()

	// Track component behavior
	var spiderStopped atomic.Bool
	var reducerRan atomic.Bool
	var keyWatcherActive atomic.Bool

	// Spider component - only runs in COLLECTING state
	spiderLoop := func(ctx context.Context) {
		for {
			select {
			case <-ctx.Done():
				return
			default:
				if !sm.IsCollecting() {
					spiderStopped.Store(true)
					time.Sleep(10 * time.Millisecond)
					continue
				}
				spiderStopped.Store(false)
				time.Sleep(5 * time.Millisecond)
			}
		}
	}

	// Reducer component - only runs in REDUCING state
	reducerLoop := func(ctx context.Context) {
		for {
			select {
			case <-ctx.Done():
				return
			default:
				if sm.Current() == StateReducing {
					warmLock.Lock()
					reducerRan.Store(true)
					time.Sleep(10 * time.Millisecond)
					warmLock.Unlock()
				}
				time.Sleep(5 * time.Millisecond)
			}
		}
	}

	// Key watcher - only active in WAITING_FOR_KEY state
	keyWatcherLoop := func(ctx context.Context) {
		for {
			select {
			case <-ctx.Done():
				return
			default:
				if sm.Current() == StateWaitingForKey {
					keyWatcherActive.Store(true)
				} else {
					keyWatcherActive.Store(false)
				}
				time.Sleep(5 * time.Millisecond)
			}
		}
	}

	// Start all component loops
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go spiderLoop(ctx)
	go reducerLoop(ctx)
	go keyWatcherLoop(ctx)

	// Test 1: In STARTUP, spider should be stopped
	time.Sleep(50 * time.Millisecond)
	if !spiderStopped.Load() {
		t.Error("spider should be stopped in STARTUP")
	}

	// Test 2: Transition to COLLECTING, spider should run
	sm.TransitionTo(StateCollecting)
	time.Sleep(50 * time.Millisecond)
	if spiderStopped.Load() {
		t.Error("spider should be running in COLLECTING")
	}
	if reducerRan.Load() {
		reducerRan.Store(false) // Reset for next check
	}

	// Test 3: Transition to REDUCING, spider stops, reducer runs
	sm.TryTransitionToReducing()
	time.Sleep(50 * time.Millisecond)
	if !spiderStopped.Load() {
		t.Error("spider should stop in REDUCING")
	}
	if !reducerRan.Load() {
		t.Error("reducer should run in REDUCING")
	}

	// Test 4: Transition through PUSHING to WAITING_FOR_KEY
	sm.TransitionTo(StatePushing)
	sm.TransitionTo(StateWaitingForKey)
	time.Sleep(50 * time.Millisecond)
	if !keyWatcherActive.Load() {
		t.Error("key watcher should be active in WAITING_FOR_KEY")
	}

	// Test 5: After fresh restart, back to normal operation
	sm.TransitionTo(StateFreshRestart)
	time.Sleep(20 * time.Millisecond)
	if keyWatcherActive.Load() {
		t.Error("key watcher should be inactive after WAITING_FOR_KEY")
	}
}

// TestContinuousCollector_Integration_ReducerWithWarmLock tests that the reducer
// properly acquires the warm lock during reduce operations
func TestContinuousCollector_Integration_ReducerWithWarmLock(t *testing.T) {
	sm := NewStateMachine()
	warmLock := NewWarmLock()

	var rotationBlocked atomic.Bool
	var rotationSucceeded atomic.Int32
	var reducerAcquiredLock atomic.Bool

	// Start collecting
	sm.TransitionTo(StateCollecting)

	// Start multiple rotation goroutines (simulating spider/rotator)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	rotatorLoop := func(id int) {
		for {
			select {
			case <-ctx.Done():
				return
			default:
				if sm.IsCollecting() {
					// Try to acquire read lock for rotation
					warmLock.RLock()
					rotationSucceeded.Add(1)
					time.Sleep(time.Millisecond)
					warmLock.RUnlock()
				} else {
					// In REDUCING state, try RLock to see if blocked
					start := time.Now()
					warmLock.RLock()
					if time.Since(start) > 5*time.Millisecond {
						rotationBlocked.Store(true)
					}
					warmLock.RUnlock()
				}
				time.Sleep(5 * time.Millisecond)
			}
		}
	}

	// Start 5 rotators
	for i := 0; i < 5; i++ {
		go rotatorLoop(i)
	}

	// Let rotators run
	time.Sleep(50 * time.Millisecond)

	initialRotations := rotationSucceeded.Load()
	if initialRotations == 0 {
		t.Error("expected some rotations to succeed during COLLECTING")
	}

	// Transition to REDUCING
	sm.TryTransitionToReducing()

	// Reducer acquires exclusive lock
	go func() {
		warmLock.Lock()
		reducerAcquiredLock.Store(true)

		// Hold lock for a while (simulating reduce operation)
		time.Sleep(100 * time.Millisecond)

		warmLock.Unlock()
	}()

	// Wait for reducer to acquire lock
	time.Sleep(20 * time.Millisecond)

	if !reducerAcquiredLock.Load() {
		t.Error("reducer should have acquired lock")
	}

	// While reducer holds lock, rotations should be blocked
	rotationsBeforeLockRelease := rotationSucceeded.Load()
	time.Sleep(50 * time.Millisecond) // While lock is held
	rotationsAfterSomeWait := rotationSucceeded.Load()

	// Rotations should not increase significantly while lock is held
	// (they might get a few in before the lock, so we allow some slack)
	if rotationsAfterSomeWait-rotationsBeforeLockRelease > 10 {
		t.Errorf("rotations increased from %d to %d while reducer held lock",
			rotationsBeforeLockRelease, rotationsAfterSomeWait)
	}
}

// TestContinuousCollector_Integration_MultipleReduceCycles tests running
// multiple complete reduce cycles
func TestContinuousCollector_Integration_MultipleReduceCycles(t *testing.T) {
	sm := NewStateMachine()

	var reduceCycles atomic.Int32

	counter := NewWarmFileCounter(5, func() {
		if sm.TryTransitionToReducing() {
			// Simulate reduce cycle
			reduceCycles.Add(1)

			// Complete reduce cycle: REDUCING → PUSHING → COLLECTING
			sm.TransitionTo(StatePushing)
			time.Sleep(5 * time.Millisecond) // Simulate push
			sm.TransitionTo(StateCollecting)
		}
	})

	// Start collecting
	sm.TransitionTo(StateCollecting)

	// Run 10 reduce cycles
	for cycle := 0; cycle < 10; cycle++ {
		// Reset counter for new cycle
		counter.Reset()

		// Increment to threshold
		for i := 0; i < 5; i++ {
			counter.Increment()
		}

		// Wait for cycle to complete
		time.Sleep(20 * time.Millisecond)
	}

	if reduceCycles.Load() != 10 {
		t.Errorf("reduce cycles = %d, want 10", reduceCycles.Load())
	}

	// Should end in COLLECTING state
	if sm.Current() != StateCollecting {
		t.Errorf("final state = %v, want %v", sm.Current(), StateCollecting)
	}
}
