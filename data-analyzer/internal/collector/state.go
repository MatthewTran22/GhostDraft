package collector

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// State represents the current state of the continuous collector.
type State int32

const (
	StateStartup       State = iota // Initial state, seeding from Challenger
	StateCollecting                 // Actively collecting matches
	StateReducing                   // Aggregating warm files, archiving to cold
	StatePushing                    // Pushing aggregated data to Turso
	StateWaitingForKey              // API key expired, waiting for new key
	StateFreshRestart               // Clearing state for new session
	StateShutdown                   // Graceful shutdown in progress
)

// String returns the string representation of the state.
func (s State) String() string {
	switch s {
	case StateStartup:
		return "STARTUP"
	case StateCollecting:
		return "COLLECTING"
	case StateReducing:
		return "REDUCING"
	case StatePushing:
		return "PUSHING"
	case StateWaitingForKey:
		return "WAITING_FOR_KEY"
	case StateFreshRestart:
		return "FRESH_RESTART"
	case StateShutdown:
		return "SHUTDOWN"
	default:
		return fmt.Sprintf("UNKNOWN(%d)", s)
	}
}

// InvalidTransitionError is returned when an invalid state transition is attempted.
type InvalidTransitionError struct {
	From State
	To   State
}

func (e *InvalidTransitionError) Error() string {
	return fmt.Sprintf("invalid state transition from %s to %s", e.From, e.To)
}

// validTransitions defines the allowed state transitions.
// Key: from state, Value: set of valid target states.
var validTransitions = map[State]map[State]bool{
	StateStartup: {
		StateCollecting: true,
		StateShutdown:   true,
	},
	StateCollecting: {
		StateReducing: true,
		StateShutdown: true,
	},
	StateReducing: {
		StatePushing:  true,
		StateShutdown: true,
	},
	StatePushing: {
		StateCollecting:    true,
		StateWaitingForKey: true,
		StateShutdown:      true,
	},
	StateWaitingForKey: {
		StateFreshRestart: true,
		StateShutdown:     true,
	},
	StateFreshRestart: {
		StateStartup:  true,
		StateShutdown: true,
	},
	StateShutdown: {
		// No valid transitions from shutdown
	},
}

// StateMachine manages state transitions for the continuous collector.
type StateMachine struct {
	state      atomic.Int32
	mu         sync.RWMutex
	callback   func(from, to State)
	cond       *sync.Cond
	condMu     sync.Mutex
}

// NewStateMachine creates a new state machine starting in STARTUP state.
func NewStateMachine() *StateMachine {
	sm := &StateMachine{}
	sm.state.Store(int32(StateStartup))
	sm.cond = sync.NewCond(&sm.condMu)
	return sm
}

// Current returns the current state.
func (sm *StateMachine) Current() State {
	return State(sm.state.Load())
}

// setState sets the state directly (for testing purposes).
func (sm *StateMachine) setState(s State) {
	sm.state.Store(int32(s))
}

// TransitionTo attempts to transition to the target state.
// Returns an error if the transition is not valid.
func (sm *StateMachine) TransitionTo(to State) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	from := State(sm.state.Load())

	// Check if transition is valid
	if !sm.isValidTransition(from, to) {
		return &InvalidTransitionError{From: from, To: to}
	}

	// Perform transition
	sm.state.Store(int32(to))

	// Notify waiters
	sm.cond.Broadcast()

	// Call callback if set
	if sm.callback != nil {
		sm.callback(from, to)
	}

	return nil
}

// isValidTransition checks if a transition from one state to another is valid.
func (sm *StateMachine) isValidTransition(from, to State) bool {
	// Can't transition to the same state
	if from == to {
		return false
	}

	targets, ok := validTransitions[from]
	if !ok {
		return false
	}

	return targets[to]
}

// IsCollecting returns true if the state machine is in COLLECTING state.
func (sm *StateMachine) IsCollecting() bool {
	return sm.Current() == StateCollecting
}

// IsIdle returns true if the state machine is in an idle state
// (not actively processing: STARTUP, COLLECTING, or WAITING_FOR_KEY).
func (sm *StateMachine) IsIdle() bool {
	current := sm.Current()
	return current == StateStartup || current == StateCollecting || current == StateWaitingForKey
}

// CanReduce returns true if the state machine can transition to REDUCING.
func (sm *StateMachine) CanReduce() bool {
	return sm.Current() == StateCollecting
}

// TryTransitionToReducing attempts to atomically transition from COLLECTING to REDUCING.
// Returns true if the transition was successful, false otherwise.
// This is safe to call concurrently - only one caller will succeed.
func (sm *StateMachine) TryTransitionToReducing() bool {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	from := State(sm.state.Load())
	if from != StateCollecting {
		return false
	}

	to := StateReducing
	sm.state.Store(int32(to))

	// Notify waiters
	sm.cond.Broadcast()

	// Call callback if set
	if sm.callback != nil {
		sm.callback(from, to)
	}

	return true
}

// OnTransition sets a callback to be called after each successful transition.
func (sm *StateMachine) OnTransition(callback func(from, to State)) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.callback = callback
}

// WaitForState blocks until the state machine reaches the target state or times out.
// Returns true if the state was reached, false if the timeout expired.
func (sm *StateMachine) WaitForState(target State, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)

	sm.condMu.Lock()
	defer sm.condMu.Unlock()

	for sm.Current() != target {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return false
		}

		// Wait with timeout using a goroutine
		done := make(chan struct{})
		go func() {
			time.Sleep(remaining)
			sm.cond.Broadcast()
			close(done)
		}()

		sm.cond.Wait()

		select {
		case <-done:
			// Timeout goroutine finished, check if we reached target
			if sm.Current() != target {
				return false
			}
		default:
			// Woken up by state change
		}
	}

	return true
}
