package collector

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestState_ValidValues(t *testing.T) {
	// Test that all state values are defined and unique
	states := []State{
		StateStartup,
		StateCollecting,
		StateReducing,
		StatePushing,
		StateWaitingForKey,
		StateFreshRestart,
		StateShutdown,
	}

	seen := make(map[State]bool)
	for _, s := range states {
		if seen[s] {
			t.Errorf("duplicate state value: %v", s)
		}
		seen[s] = true
	}

	if len(states) != 7 {
		t.Errorf("expected 7 states, got %d", len(states))
	}
}

func TestState_StringRepresentation(t *testing.T) {
	tests := []struct {
		state    State
		expected string
	}{
		{StateStartup, "STARTUP"},
		{StateCollecting, "COLLECTING"},
		{StateReducing, "REDUCING"},
		{StatePushing, "PUSHING"},
		{StateWaitingForKey, "WAITING_FOR_KEY"},
		{StateFreshRestart, "FRESH_RESTART"},
		{StateShutdown, "SHUTDOWN"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.state.String(); got != tt.expected {
				t.Errorf("State.String() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestStateMachine_InitialState(t *testing.T) {
	sm := NewStateMachine()
	if sm.Current() != StateStartup {
		t.Errorf("initial state = %v, want %v", sm.Current(), StateStartup)
	}
}

func TestStateMachine_ValidTransitions(t *testing.T) {
	validTransitions := []struct {
		name string
		from State
		to   State
	}{
		// Normal flow
		{"startup to collecting", StateStartup, StateCollecting},
		{"collecting to reducing", StateCollecting, StateReducing},
		{"reducing to pushing", StateReducing, StatePushing},
		{"pushing to collecting", StatePushing, StateCollecting},

		// Key expiration flow
		{"pushing to waiting", StatePushing, StateWaitingForKey},
		{"waiting to fresh restart", StateWaitingForKey, StateFreshRestart},
		{"fresh restart to startup", StateFreshRestart, StateStartup},

		// Shutdown from any active state
		{"collecting to shutdown", StateCollecting, StateShutdown},
		{"reducing to shutdown", StateReducing, StateShutdown},
		{"pushing to shutdown", StatePushing, StateShutdown},
		{"waiting to shutdown", StateWaitingForKey, StateShutdown},
	}

	for _, tt := range validTransitions {
		t.Run(tt.name, func(t *testing.T) {
			sm := NewStateMachine()
			sm.setState(tt.from) // helper to set state directly for testing

			err := sm.TransitionTo(tt.to)
			if err != nil {
				t.Errorf("TransitionTo(%v) from %v returned error: %v", tt.to, tt.from, err)
			}
			if sm.Current() != tt.to {
				t.Errorf("after transition, state = %v, want %v", sm.Current(), tt.to)
			}
		})
	}
}

func TestStateMachine_InvalidTransitions(t *testing.T) {
	invalidTransitions := []struct {
		name string
		from State
		to   State
	}{
		// Can't skip states
		{"startup to reducing", StateStartup, StateReducing},
		{"startup to pushing", StateStartup, StatePushing},
		{"collecting to pushing", StateCollecting, StatePushing},
		{"collecting to waiting", StateCollecting, StateWaitingForKey},

		// Can't go backwards in normal flow
		{"reducing to collecting", StateReducing, StateCollecting},
		{"pushing to reducing", StatePushing, StateReducing},

		// Can't exit shutdown
		{"shutdown to collecting", StateShutdown, StateCollecting},
		{"shutdown to startup", StateShutdown, StateStartup},

		// Can't transition to same state
		{"collecting to collecting", StateCollecting, StateCollecting},
		{"reducing to reducing", StateReducing, StateReducing},
	}

	for _, tt := range invalidTransitions {
		t.Run(tt.name, func(t *testing.T) {
			sm := NewStateMachine()
			sm.setState(tt.from)

			err := sm.TransitionTo(tt.to)
			if err == nil {
				t.Errorf("TransitionTo(%v) from %v should return error", tt.to, tt.from)
			}
			// State should remain unchanged on invalid transition
			if sm.Current() != tt.from {
				t.Errorf("state changed to %v on invalid transition, should remain %v", sm.Current(), tt.from)
			}
		})
	}
}

func TestStateMachine_TransitionError(t *testing.T) {
	sm := NewStateMachine()
	sm.setState(StateCollecting)

	err := sm.TransitionTo(StateWaitingForKey)
	if err == nil {
		t.Fatal("expected error for invalid transition")
	}

	// Check error is of correct type
	var transErr *InvalidTransitionError
	if !errors.As(err, &transErr) {
		t.Errorf("error should be InvalidTransitionError, got %T", err)
	}

	if transErr != nil {
		if transErr.From != StateCollecting {
			t.Errorf("error.From = %v, want %v", transErr.From, StateCollecting)
		}
		if transErr.To != StateWaitingForKey {
			t.Errorf("error.To = %v, want %v", transErr.To, StateWaitingForKey)
		}
	}
}

func TestStateMachine_IsCollecting(t *testing.T) {
	sm := NewStateMachine()

	sm.setState(StateStartup)
	if sm.IsCollecting() {
		t.Error("IsCollecting() should be false in STARTUP")
	}

	sm.setState(StateCollecting)
	if !sm.IsCollecting() {
		t.Error("IsCollecting() should be true in COLLECTING")
	}

	sm.setState(StateReducing)
	if sm.IsCollecting() {
		t.Error("IsCollecting() should be false in REDUCING")
	}
}

func TestStateMachine_IsIdle(t *testing.T) {
	sm := NewStateMachine()

	// Idle states: STARTUP, COLLECTING, WAITING_FOR_KEY
	idleStates := []State{StateStartup, StateCollecting, StateWaitingForKey}
	for _, s := range idleStates {
		sm.setState(s)
		if !sm.IsIdle() {
			t.Errorf("IsIdle() should be true in %v", s)
		}
	}

	// Non-idle states: REDUCING, PUSHING, FRESH_RESTART, SHUTDOWN
	busyStates := []State{StateReducing, StatePushing, StateFreshRestart, StateShutdown}
	for _, s := range busyStates {
		sm.setState(s)
		if sm.IsIdle() {
			t.Errorf("IsIdle() should be false in %v", s)
		}
	}
}

func TestStateMachine_CanReduce(t *testing.T) {
	tests := []struct {
		state    State
		expected bool
	}{
		{StateStartup, false},
		{StateCollecting, true},
		{StateReducing, false},
		{StatePushing, false},
		{StateWaitingForKey, false},
		{StateFreshRestart, false},
		{StateShutdown, false},
	}

	for _, tt := range tests {
		t.Run(tt.state.String(), func(t *testing.T) {
			sm := NewStateMachine()
			sm.setState(tt.state)

			if got := sm.CanReduce(); got != tt.expected {
				t.Errorf("CanReduce() in %v = %v, want %v", tt.state, got, tt.expected)
			}
		})
	}
}

func TestStateMachine_TryTransitionToReducing(t *testing.T) {
	t.Run("succeeds from collecting", func(t *testing.T) {
		sm := NewStateMachine()
		sm.setState(StateCollecting)

		ok := sm.TryTransitionToReducing()
		if !ok {
			t.Error("TryTransitionToReducing() should succeed from COLLECTING")
		}
		if sm.Current() != StateReducing {
			t.Errorf("state = %v, want %v", sm.Current(), StateReducing)
		}
	})

	t.Run("fails from other states", func(t *testing.T) {
		otherStates := []State{StateStartup, StateReducing, StatePushing, StateWaitingForKey}
		for _, s := range otherStates {
			sm := NewStateMachine()
			sm.setState(s)

			ok := sm.TryTransitionToReducing()
			if ok {
				t.Errorf("TryTransitionToReducing() should fail from %v", s)
			}
			if sm.Current() != s {
				t.Errorf("state changed from %v to %v on failed transition", s, sm.Current())
			}
		}
	})
}

func TestStateMachine_ConcurrentTransitions(t *testing.T) {
	sm := NewStateMachine()
	sm.setState(StateCollecting)

	// Multiple goroutines try to transition to REDUCING simultaneously
	// Only one should succeed
	const numGoroutines = 100
	var wg sync.WaitGroup
	successCount := int32(0)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if sm.TryTransitionToReducing() {
				atomic.AddInt32(&successCount, 1)
			}
		}()
	}

	wg.Wait()

	if successCount != 1 {
		t.Errorf("expected exactly 1 successful transition, got %d", successCount)
	}
	if sm.Current() != StateReducing {
		t.Errorf("final state = %v, want %v", sm.Current(), StateReducing)
	}
}

func TestStateMachine_OnTransition_Callback(t *testing.T) {
	sm := NewStateMachine()

	var callbackCalls []struct {
		from State
		to   State
	}

	sm.OnTransition(func(from, to State) {
		callbackCalls = append(callbackCalls, struct {
			from State
			to   State
		}{from, to})
	})

	// Perform transitions
	sm.TransitionTo(StateCollecting)
	sm.TransitionTo(StateReducing)
	sm.TransitionTo(StatePushing)

	if len(callbackCalls) != 3 {
		t.Errorf("expected 3 callback calls, got %d", len(callbackCalls))
	}

	// Verify first transition
	if callbackCalls[0].from != StateStartup || callbackCalls[0].to != StateCollecting {
		t.Errorf("first callback: from=%v, to=%v; want from=%v, to=%v",
			callbackCalls[0].from, callbackCalls[0].to, StateStartup, StateCollecting)
	}
}

func TestStateMachine_WaitForState(t *testing.T) {
	sm := NewStateMachine()

	// Start a goroutine that transitions after a delay
	go func() {
		time.Sleep(50 * time.Millisecond)
		sm.TransitionTo(StateCollecting)
	}()

	// Wait should return when state is reached
	start := time.Now()
	sm.WaitForState(StateCollecting, 1*time.Second)
	elapsed := time.Since(start)

	if sm.Current() != StateCollecting {
		t.Errorf("state = %v, want %v", sm.Current(), StateCollecting)
	}

	if elapsed > 500*time.Millisecond {
		t.Errorf("WaitForState took too long: %v", elapsed)
	}
}

func TestStateMachine_WaitForState_Timeout(t *testing.T) {
	sm := NewStateMachine()

	// Don't transition - should timeout
	start := time.Now()
	reached := sm.WaitForState(StateCollecting, 100*time.Millisecond)
	elapsed := time.Since(start)

	if reached {
		t.Error("WaitForState should return false on timeout")
	}

	if elapsed < 100*time.Millisecond {
		t.Errorf("WaitForState returned too early: %v", elapsed)
	}
}
