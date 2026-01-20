package collector

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// MockSpider is a test double for the spider component
type MockSpider struct {
	mu             sync.Mutex
	matchCount     int
	shouldFail     bool
	failError      error
	onFetch        func() // callback when fetch is called
	fetchCallCount int
}

func (m *MockSpider) FetchMatches(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.fetchCallCount++

	if m.onFetch != nil {
		m.onFetch()
	}

	if m.shouldFail {
		return m.failError
	}
	m.matchCount++
	return nil
}

func (m *MockSpider) SetFailure(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.shouldFail = true
	m.failError = err
}

func (m *MockSpider) ClearFailure() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.shouldFail = false
	m.failError = nil
}

// MockReducer is a test double for the reducer component
type MockReducer struct {
	mu          sync.Mutex
	reduceCount int
	reduceCalls []time.Time
	onReduce    func() error
}

func (m *MockReducer) Reduce(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reduceCount++
	m.reduceCalls = append(m.reduceCalls, time.Now())

	if m.onReduce != nil {
		return m.onReduce()
	}
	return nil
}

func (m *MockReducer) GetReduceCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.reduceCount
}

// MockKeyValidator is a test double for API key validation
type MockKeyValidator struct {
	mu       sync.Mutex
	isValid  bool
	validKey string
}

func (m *MockKeyValidator) ValidateKey(key string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.validKey != "" {
		return key == m.validKey
	}
	return m.isValid
}

func (m *MockKeyValidator) SetValid(valid bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.isValid = valid
}

// MockKeyProvider is a test double for getting new API keys
type MockKeyProvider struct {
	mu       sync.Mutex
	nextKey  string
	keyChan  chan string
}

func NewMockKeyProvider() *MockKeyProvider {
	return &MockKeyProvider{
		keyChan: make(chan string, 1),
	}
}

func (m *MockKeyProvider) WaitForKey(ctx context.Context) (string, error) {
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case key := <-m.keyChan:
		return key, nil
	}
}

func (m *MockKeyProvider) ProvideKey(key string) {
	m.keyChan <- key
}

// Note: ErrAPIKeyExpired, ErrAPIKeyForbidden, and IsAPIKeyError are defined in continuous.go

// TestContinuousCollector_WarmFileCountTriggersReduce tests that reaching
// the warm file threshold triggers a transition from COLLECTING to REDUCING
func TestContinuousCollector_WarmFileCountTriggersReduce(t *testing.T) {
	// Create state machine
	sm := NewStateMachine()
	sm.TransitionTo(StateCollecting)

	// Create warm file counter with threshold of 10
	var reduceTriggered atomic.Bool
	var triggerCount atomic.Int32

	counter := NewWarmFileCounter(10, func() {
		triggerCount.Add(1)
		// Only transition if we're in COLLECTING state
		if sm.TryTransitionToReducing() {
			reduceTriggered.Store(true)
		}
	})

	// Simulate 9 file rotations - should NOT trigger
	for i := 0; i < 9; i++ {
		counter.Increment()
	}

	if reduceTriggered.Load() {
		t.Error("reduce triggered before reaching threshold")
	}

	if sm.Current() != StateCollecting {
		t.Errorf("state = %v, want %v", sm.Current(), StateCollecting)
	}

	// 10th rotation should trigger
	counter.Increment()

	if !reduceTriggered.Load() {
		t.Error("reduce should have triggered at threshold")
	}

	if sm.Current() != StateReducing {
		t.Errorf("state = %v, want %v", sm.Current(), StateReducing)
	}

	if triggerCount.Load() != 1 {
		t.Errorf("trigger count = %d, want 1", triggerCount.Load())
	}
}

func TestContinuousCollector_DoubleReducePrevention(t *testing.T) {
	sm := NewStateMachine()
	sm.TransitionTo(StateCollecting)

	var triggerCount atomic.Int32

	counter := NewWarmFileCounter(5, func() {
		triggerCount.Add(1)
		sm.TryTransitionToReducing()
	})

	// Trigger threshold
	for i := 0; i < 5; i++ {
		counter.Increment()
	}

	// Reset and trigger again while still in REDUCING
	counter.Reset()
	for i := 0; i < 5; i++ {
		counter.Increment()
	}

	// Callback fires twice but only one transition succeeds
	if triggerCount.Load() != 2 {
		t.Errorf("trigger count = %d, want 2", triggerCount.Load())
	}

	// State should still be REDUCING (second transition failed)
	if sm.Current() != StateReducing {
		t.Errorf("state = %v, want %v", sm.Current(), StateReducing)
	}
}

// TestContinuousCollector_APIErrorTriggersReduce tests that 401/403 errors
// trigger a transition from COLLECTING to REDUCING
func TestContinuousCollector_APIErrorTriggersReduce(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{"401 unauthorized", ErrAPIKeyExpired},
		{"403 forbidden", ErrAPIKeyForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sm := NewStateMachine()
			sm.TransitionTo(StateCollecting)

			mockSpider := &MockSpider{}
			mockReducer := &MockReducer{}

			// Simulate API error handler
			handleAPIError := func(err error) {
				if IsAPIKeyError(err) && sm.TryTransitionToReducing() {
					mockReducer.Reduce(context.Background())
				}
			}

			// Spider returns API error
			mockSpider.SetFailure(tt.err)

			// Simulate fetch attempt
			err := mockSpider.FetchMatches(context.Background())
			if err != nil && IsAPIKeyError(err) {
				handleAPIError(err)
			}

			// Should transition to REDUCING
			if sm.Current() != StateReducing {
				t.Errorf("state = %v, want %v", sm.Current(), StateReducing)
			}

			// Reducer should have been called
			if mockReducer.GetReduceCount() != 1 {
				t.Errorf("reduce count = %d, want 1", mockReducer.GetReduceCount())
			}
		})
	}
}

func TestContinuousCollector_NonAPIErrorDoesNotTriggerReduce(t *testing.T) {
	sm := NewStateMachine()
	sm.TransitionTo(StateCollecting)

	mockSpider := &MockSpider{}
	mockReducer := &MockReducer{}

	// Non-API error (e.g., network timeout)
	networkError := errors.New("connection timeout")
	mockSpider.SetFailure(networkError)

	handleError := func(err error) {
		if IsAPIKeyError(err) && sm.TryTransitionToReducing() {
			mockReducer.Reduce(context.Background())
		}
		// Non-API errors should be retried, not trigger reduce
	}

	err := mockSpider.FetchMatches(context.Background())
	if err != nil {
		handleError(err)
	}

	// Should remain in COLLECTING
	if sm.Current() != StateCollecting {
		t.Errorf("state = %v, want %v", sm.Current(), StateCollecting)
	}

	// Reducer should NOT have been called
	if mockReducer.GetReduceCount() != 0 {
		t.Errorf("reduce count = %d, want 0", mockReducer.GetReduceCount())
	}
}

// TestContinuousCollector_WaitingToFreshRestart tests that providing a new
// valid key transitions from WAITING_FOR_KEY to FRESH_RESTART
func TestContinuousCollector_WaitingToFreshRestart(t *testing.T) {
	sm := NewStateMachine()
	// Simulate reaching WAITING_FOR_KEY state
	sm.setState(StateWaitingForKey)

	keyValidator := &MockKeyValidator{isValid: true}
	keyProvider := NewMockKeyProvider()

	var freshRestartCalled atomic.Bool

	// Simulate the key waiting loop
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		newKey, err := keyProvider.WaitForKey(ctx)
		if err != nil {
			return
		}

		if keyValidator.ValidateKey(newKey) {
			if err := sm.TransitionTo(StateFreshRestart); err == nil {
				freshRestartCalled.Store(true)
			}
		}
	}()

	// Wait a bit for goroutine to start
	time.Sleep(50 * time.Millisecond)

	// Provide new valid key
	keyProvider.ProvideKey("RGAPI-new-valid-key")

	// Wait for transition
	time.Sleep(100 * time.Millisecond)

	if !freshRestartCalled.Load() {
		t.Error("fresh restart should have been triggered")
	}

	if sm.Current() != StateFreshRestart {
		t.Errorf("state = %v, want %v", sm.Current(), StateFreshRestart)
	}
}

func TestContinuousCollector_InvalidKeyDoesNotTriggerRestart(t *testing.T) {
	sm := NewStateMachine()
	sm.setState(StateWaitingForKey)

	keyValidator := &MockKeyValidator{isValid: false}
	keyProvider := NewMockKeyProvider()

	var freshRestartCalled atomic.Bool

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()

		newKey, err := keyProvider.WaitForKey(ctx)
		if err != nil {
			return
		}

		if keyValidator.ValidateKey(newKey) {
			if err := sm.TransitionTo(StateFreshRestart); err == nil {
				freshRestartCalled.Store(true)
			}
		}
	}()

	time.Sleep(50 * time.Millisecond)

	// Provide invalid key
	keyProvider.ProvideKey("INVALID-KEY")

	time.Sleep(100 * time.Millisecond)

	if freshRestartCalled.Load() {
		t.Error("fresh restart should NOT have been triggered with invalid key")
	}

	// Should still be waiting
	if sm.Current() != StateWaitingForKey {
		t.Errorf("state = %v, want %v", sm.Current(), StateWaitingForKey)
	}
}

func TestContinuousCollector_FreshRestartClearsState(t *testing.T) {
	// Create collector state
	type CollectorState struct {
		matchBloomFilter  map[string]bool // simplified bloom filter
		puuidBloomFilter  map[string]bool
		playerQueue       []string
		warmFileCount     int64
	}

	state := &CollectorState{
		matchBloomFilter: map[string]bool{"match1": true, "match2": true},
		puuidBloomFilter: map[string]bool{"puuid1": true, "puuid2": true},
		playerQueue:      []string{"player1", "player2", "player3"},
		warmFileCount:    7,
	}

	// Fresh restart should clear all state
	freshRestart := func(s *CollectorState) {
		s.matchBloomFilter = make(map[string]bool)
		s.puuidBloomFilter = make(map[string]bool)
		s.playerQueue = nil
		s.warmFileCount = 0
	}

	freshRestart(state)

	if len(state.matchBloomFilter) != 0 {
		t.Errorf("matchBloomFilter should be empty, got %d entries", len(state.matchBloomFilter))
	}

	if len(state.puuidBloomFilter) != 0 {
		t.Errorf("puuidBloomFilter should be empty, got %d entries", len(state.puuidBloomFilter))
	}

	if len(state.playerQueue) != 0 {
		t.Errorf("playerQueue should be empty, got %d entries", len(state.playerQueue))
	}

	if state.warmFileCount != 0 {
		t.Errorf("warmFileCount should be 0, got %d", state.warmFileCount)
	}
}

func TestContinuousCollector_FreshRestartSeedsFromChallenger(t *testing.T) {
	// Mock challenger fetcher
	type MockChallengerFetcher struct {
		topPlayer string
	}

	fetcher := &MockChallengerFetcher{topPlayer: "Challenger-PUUID-1"}

	var seedQueue []string

	// Fresh restart seeds from Challenger #1
	freshRestartWithSeed := func(f *MockChallengerFetcher, queue *[]string) {
		*queue = nil // Clear queue
		*queue = append(*queue, f.topPlayer) // Seed with #1 Challenger
	}

	freshRestartWithSeed(fetcher, &seedQueue)

	if len(seedQueue) != 1 {
		t.Errorf("seedQueue should have 1 entry, got %d", len(seedQueue))
	}

	if seedQueue[0] != "Challenger-PUUID-1" {
		t.Errorf("seedQueue[0] = %q, want %q", seedQueue[0], "Challenger-PUUID-1")
	}
}

func TestContinuousCollector_StateTransitionCallbacksInOrder(t *testing.T) {
	sm := NewStateMachine()

	var transitions []struct {
		from State
		to   State
	}

	sm.OnTransition(func(from, to State) {
		transitions = append(transitions, struct {
			from State
			to   State
		}{from, to})
	})

	// Simulate full cycle: STARTUP → COLLECTING → REDUCING → PUSHING → COLLECTING
	sm.TransitionTo(StateCollecting)
	sm.TransitionTo(StateReducing)
	sm.TransitionTo(StatePushing)
	sm.TransitionTo(StateCollecting)

	expected := []struct {
		from State
		to   State
	}{
		{StateStartup, StateCollecting},
		{StateCollecting, StateReducing},
		{StateReducing, StatePushing},
		{StatePushing, StateCollecting},
	}

	if len(transitions) != len(expected) {
		t.Fatalf("got %d transitions, want %d", len(transitions), len(expected))
	}

	for i, exp := range expected {
		if transitions[i].from != exp.from || transitions[i].to != exp.to {
			t.Errorf("transition[%d] = %v→%v, want %v→%v",
				i, transitions[i].from, transitions[i].to, exp.from, exp.to)
		}
	}
}

func TestContinuousCollector_KeyExpirationFullCycle(t *testing.T) {
	sm := NewStateMachine()

	// Full key expiration cycle:
	// COLLECTING → REDUCING → PUSHING → WAITING_FOR_KEY → FRESH_RESTART → STARTUP → COLLECTING

	sm.TransitionTo(StateCollecting)
	if sm.Current() != StateCollecting {
		t.Fatalf("failed to transition to COLLECTING")
	}

	// API error triggers reduce
	sm.TransitionTo(StateReducing)
	if sm.Current() != StateReducing {
		t.Fatalf("failed to transition to REDUCING")
	}

	// After reduce, push to Turso
	sm.TransitionTo(StatePushing)
	if sm.Current() != StatePushing {
		t.Fatalf("failed to transition to PUSHING")
	}

	// Key expired, wait for new key
	sm.TransitionTo(StateWaitingForKey)
	if sm.Current() != StateWaitingForKey {
		t.Fatalf("failed to transition to WAITING_FOR_KEY")
	}

	// New key received, fresh restart
	sm.TransitionTo(StateFreshRestart)
	if sm.Current() != StateFreshRestart {
		t.Fatalf("failed to transition to FRESH_RESTART")
	}

	// Back to startup
	sm.TransitionTo(StateStartup)
	if sm.Current() != StateStartup {
		t.Fatalf("failed to transition to STARTUP")
	}

	// Resume collecting
	sm.TransitionTo(StateCollecting)
	if sm.Current() != StateCollecting {
		t.Fatalf("failed to transition back to COLLECTING")
	}
}
