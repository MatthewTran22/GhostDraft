package collector

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// SpiderRunner is the interface for the match fetching component.
// Note: This is separate from the Spider struct to allow for mocking in tests.
type SpiderRunner interface {
	// RunContinuous starts the spider's collection loop. It should respect the context
	// for cancellation and return when the context is cancelled.
	RunContinuous(ctx context.Context) error
	// Reset clears internal state (bloom filters, player queue)
	Reset()
	// SeedFromChallenger seeds the spider with the top Challenger player
	SeedFromChallenger(ctx context.Context) error
}

// API Key error types
var (
	ErrAPIKeyExpired      = errors.New("api key expired (401)")
	ErrAPIKeyForbidden    = errors.New("api key forbidden (403)")
	ErrCollectionComplete = errors.New("collection complete (max players reached)")
)

// IsAPIKeyError checks if an error indicates API key expiration (401 or 403)
func IsAPIKeyError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrAPIKeyExpired) || errors.Is(err, ErrAPIKeyForbidden) {
		return true
	}
	// Check for HTTP status codes in error message
	errStr := err.Error()
	return contains(errStr, "401") || contains(errStr, "403") ||
		contains(errStr, "Unauthorized") || contains(errStr, "Forbidden")
}

// contains is a simple string contains helper
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsImpl(s, substr))
}

func containsImpl(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// WrapHTTPError wraps an HTTP response status code as an appropriate error
func WrapHTTPError(statusCode int, message string) error {
	switch statusCode {
	case http.StatusUnauthorized:
		return fmt.Errorf("%s: %w", message, ErrAPIKeyExpired)
	case http.StatusForbidden:
		return fmt.Errorf("%s: %w", message, ErrAPIKeyForbidden)
	default:
		return fmt.Errorf("%s: status %d", message, statusCode)
	}
}

// ReducerFunc is the function signature for the reduce operation
type ReducerFunc func(ctx context.Context) error

// KeyValidator validates API keys
type KeyValidator interface {
	ValidateKey(key string) (bool, error)
}

// KeyProvider provides new API keys (e.g., from Discord)
type KeyProvider interface {
	WaitForKey(ctx context.Context) (string, error)
}

// NotifyFunc is called to send notifications (e.g., Discord webhook)
type NotifyFunc func(ctx context.Context, message string) error

// ContinuousCollectorConfig holds configuration for the continuous collector
type ContinuousCollectorConfig struct {
	// WarmFileThreshold is the number of warm files before triggering reduce (default: 10)
	WarmFileThreshold int64
	// KeyPollInterval is how often to poll for new keys (default: 5 minutes)
	KeyPollInterval time.Duration
	// ShutdownTimeout is max time to wait for graceful shutdown (default: 5 minutes)
	ShutdownTimeout time.Duration
	// BloomResetInterval is how many reduce cycles before resetting bloom filters (default: 5)
	BloomResetInterval int
}

// DefaultConfig returns a configuration with sensible defaults
func DefaultConfig() ContinuousCollectorConfig {
	return ContinuousCollectorConfig{
		WarmFileThreshold:  10,
		KeyPollInterval:    5 * time.Minute,
		ShutdownTimeout:    5 * time.Minute,
		BloomResetInterval: 5,
	}
}

// ContinuousCollector orchestrates the continuous collection pipeline
type ContinuousCollector struct {
	// Configuration
	config ContinuousCollectorConfig

	// Core components
	stateMachine    *StateMachine
	warmFileCounter *WarmFileCounter
	warmLock        *WarmLock

	// External dependencies (injected)
	spider       SpiderRunner
	reduceFunc   ReducerFunc
	keyValidator KeyValidator
	keyProvider  KeyProvider
	notifyFunc   NotifyFunc

	// Internal state
	reduceCycleCount   atomic.Int64
	keyExpired         atomic.Bool
	collectionComplete atomic.Bool // Set when max players reached
	shutdownCh         chan struct{}
	shutdownOnce       sync.Once

	// Stats tracking for notifications
	matchesCollected atomic.Int64
	startTime        time.Time
	lastReduceTime   atomic.Value // stores time.Time

	// Synchronization
	wg sync.WaitGroup
	mu sync.Mutex
}

// NewContinuousCollector creates a new continuous collector with the given dependencies
func NewContinuousCollector(
	spider SpiderRunner,
	reduceFunc ReducerFunc,
	keyValidator KeyValidator,
	keyProvider KeyProvider,
	notifyFunc NotifyFunc,
	config ContinuousCollectorConfig,
) *ContinuousCollector {
	cc := &ContinuousCollector{
		config:       config,
		stateMachine: NewStateMachine(),
		warmLock:     NewWarmLock(),
		spider:       spider,
		reduceFunc:   reduceFunc,
		keyValidator: keyValidator,
		keyProvider:  keyProvider,
		notifyFunc:   notifyFunc,
		shutdownCh:   make(chan struct{}),
		startTime:    time.Now(),
	}
	cc.lastReduceTime.Store(time.Time{})

	// Create warm file counter with reduce trigger callback
	cc.warmFileCounter = NewWarmFileCounter(config.WarmFileThreshold, cc.onWarmFileThreshold)

	// Set up state transition callback
	cc.stateMachine.OnTransition(cc.onStateTransition)

	return cc
}

// Run starts the continuous collector and blocks until shutdown
func (cc *ContinuousCollector) Run(ctx context.Context) error {
	log.Println("[ContinuousCollector] Starting...")

	// Initial transition to COLLECTING
	if err := cc.seedAndStartCollecting(ctx); err != nil {
		return fmt.Errorf("failed to start: %w", err)
	}

	// Main loop - monitor state and handle transitions
	for {
		select {
		case <-ctx.Done():
			log.Println("[ContinuousCollector] Context cancelled, initiating shutdown...")
			cc.initiateShutdown(ctx)
			return ctx.Err()

		case <-cc.shutdownCh:
			log.Println("[ContinuousCollector] Shutdown signal received")
			return nil

		default:
			// Handle current state
			switch cc.stateMachine.Current() {
			case StateCollecting:
				// Spider is running, just wait
				time.Sleep(100 * time.Millisecond)

			case StateReducing:
				cc.handleReducing(ctx)

			case StatePushing:
				// Wait for push to complete (handled async)
				time.Sleep(100 * time.Millisecond)

			case StateWaitingForKey:
				cc.handleWaitingForKey(ctx)

			case StateFreshRestart:
				cc.handleFreshRestart(ctx)

			case StateShutdown:
				cc.wg.Wait()
				return nil

			default:
				time.Sleep(100 * time.Millisecond)
			}
		}
	}
}

// seedAndStartCollecting seeds from Challenger and starts the spider
func (cc *ContinuousCollector) seedAndStartCollecting(ctx context.Context) error {
	// Seed from Challenger #1
	if cc.spider != nil {
		if err := cc.spider.SeedFromChallenger(ctx); err != nil {
			return fmt.Errorf("failed to seed from Challenger: %w", err)
		}
	}

	// Transition to COLLECTING
	if err := cc.stateMachine.TransitionTo(StateCollecting); err != nil {
		return fmt.Errorf("failed to transition to COLLECTING: %w", err)
	}

	// Start spider in background
	cc.wg.Add(1)
	go cc.runSpider(ctx)

	return nil
}

// runSpider runs the spider loop, respecting state machine state
func (cc *ContinuousCollector) runSpider(ctx context.Context) {
	defer cc.wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		case <-cc.shutdownCh:
			return
		default:
			// Only run when in COLLECTING state
			if !cc.stateMachine.IsCollecting() {
				time.Sleep(50 * time.Millisecond)
				continue
			}

			if cc.spider == nil {
				time.Sleep(100 * time.Millisecond)
				continue
			}

			// Run spider fetch cycle
			err := cc.spider.RunContinuous(ctx)
			if err != nil {
				if cc.handleSpiderError(ctx, err) {
					// Key expired, spider should stop
					return
				}
				// Retry on other errors
				time.Sleep(time.Second)
			}
		}
	}
}

// handleSpiderError handles errors from the spider, returns true if spider should stop
func (cc *ContinuousCollector) handleSpiderError(ctx context.Context, err error) bool {
	if IsAPIKeyError(err) {
		log.Printf("[ContinuousCollector] API key expired: %v", err)
		cc.keyExpired.Store(true)
		cc.triggerReduce()
		return true
	}
	if errors.Is(err, ErrCollectionComplete) {
		log.Printf("[ContinuousCollector] Collection complete (max players reached), triggering final reduce...")
		cc.collectionComplete.Store(true)
		cc.triggerReduce()
		return true
	}
	// Log other errors but continue
	log.Printf("[ContinuousCollector] Spider error (will retry): %v", err)
	return false
}

// onWarmFileThreshold is called when warm file count reaches threshold
func (cc *ContinuousCollector) onWarmFileThreshold() {
	log.Println("[ContinuousCollector] Warm file threshold reached, triggering reduce...")
	cc.triggerReduce()
}

// triggerReduce attempts to transition to REDUCING state
func (cc *ContinuousCollector) triggerReduce() {
	if cc.stateMachine.TryTransitionToReducing() {
		log.Println("[ContinuousCollector] Transitioned to REDUCING")
	} else {
		log.Println("[ContinuousCollector] Could not transition to REDUCING (already reducing?)")
	}
}

// handleReducing executes the reduce operation
func (cc *ContinuousCollector) handleReducing(ctx context.Context) {
	log.Println("[ContinuousCollector] Executing reduce...")

	// Acquire warm lock
	cc.warmLock.Lock()

	// Execute reduce
	var reduceErr error
	if cc.reduceFunc != nil {
		reduceErr = cc.reduceFunc(ctx)
		if reduceErr != nil {
			log.Printf("[ContinuousCollector] Reduce error: %v", reduceErr)
		}
	}

	// Release lock after reduce (before Turso push)
	cc.warmLock.Unlock()

	// Record reduce completion time
	cc.lastReduceTime.Store(time.Now())

	// Increment reduce cycle count
	cycleCount := cc.reduceCycleCount.Add(1)

	// Check if we should reset bloom filters
	if cc.config.BloomResetInterval > 0 && cycleCount%int64(cc.config.BloomResetInterval) == 0 {
		log.Printf("[ContinuousCollector] Resetting bloom filters after %d cycles", cycleCount)
		if cc.spider != nil {
			cc.spider.Reset()
		}
	}

	// Reset warm file counter for next cycle
	cc.warmFileCounter.Reset()

	// Transition to PUSHING
	if err := cc.stateMachine.TransitionTo(StatePushing); err != nil {
		log.Printf("[ContinuousCollector] Failed to transition to PUSHING: %v", err)
		return
	}

	// Start async Turso push (placeholder - actual push handled elsewhere)
	cc.wg.Add(1)
	go cc.handlePushing(ctx)
}

// handlePushing completes the push phase and transitions to next state
func (cc *ContinuousCollector) handlePushing(ctx context.Context) {
	defer cc.wg.Done()

	// Turso push happens here (actual implementation would push data)
	log.Println("[ContinuousCollector] Push phase (placeholder)")

	// Determine next state based on flags
	if cc.collectionComplete.Load() {
		log.Println("[ContinuousCollector] Collection complete, initiating shutdown...")
		cc.initiateShutdown(ctx)
	} else if cc.keyExpired.Load() {
		log.Println("[ContinuousCollector] Key expired, transitioning to WAITING_FOR_KEY")
		if err := cc.stateMachine.TransitionTo(StateWaitingForKey); err != nil {
			log.Printf("[ContinuousCollector] Failed to transition to WAITING_FOR_KEY: %v", err)
		}
	} else {
		log.Println("[ContinuousCollector] Push complete, resuming collection")
		if err := cc.stateMachine.TransitionTo(StateCollecting); err != nil {
			log.Printf("[ContinuousCollector] Failed to transition to COLLECTING: %v", err)
		}
	}
}

// handleWaitingForKey waits for a new API key
func (cc *ContinuousCollector) handleWaitingForKey(ctx context.Context) {
	log.Println("[ContinuousCollector] Waiting for new API key...")

	// Send notification if available
	if cc.notifyFunc != nil {
		if err := cc.notifyFunc(ctx, "API key expired! Reply with new RGAPI-xxx key."); err != nil {
			log.Printf("[ContinuousCollector] Failed to send notification: %v", err)
		}
	}

	// Wait for new key
	if cc.keyProvider == nil {
		log.Println("[ContinuousCollector] No key provider configured, cannot continue")
		cc.initiateShutdown(ctx)
		return
	}

	pollCtx, cancel := context.WithTimeout(ctx, 24*time.Hour)
	defer cancel()

	for {
		select {
		case <-ctx.Done():
			return
		case <-cc.shutdownCh:
			return
		default:
			// Poll for new key
			newKey, err := cc.keyProvider.WaitForKey(pollCtx)
			if err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					return
				}
				log.Printf("[ContinuousCollector] Error waiting for key: %v", err)
				time.Sleep(cc.config.KeyPollInterval)
				continue
			}

			// Validate new key
			if cc.keyValidator != nil {
				valid, err := cc.keyValidator.ValidateKey(newKey)
				if err != nil {
					log.Printf("[ContinuousCollector] Error validating key: %v", err)
					continue
				}
				if !valid {
					log.Println("[ContinuousCollector] Invalid key received, continuing to wait...")
					continue
				}
			}

			// Key is valid, transition to FRESH_RESTART
			log.Println("[ContinuousCollector] Valid key received, initiating fresh restart...")
			if err := cc.stateMachine.TransitionTo(StateFreshRestart); err != nil {
				log.Printf("[ContinuousCollector] Failed to transition to FRESH_RESTART: %v", err)
				continue
			}

			// Clear key expired flag
			cc.keyExpired.Store(false)
			return
		}
	}
}

// handleFreshRestart clears state and returns to STARTUP
func (cc *ContinuousCollector) handleFreshRestart(ctx context.Context) {
	log.Println("[ContinuousCollector] Executing fresh restart...")

	// Clear all state
	if cc.spider != nil {
		cc.spider.Reset()
	}

	// Reset warm file counter
	cc.warmFileCounter.Reset()

	// Reset reduce cycle count
	cc.reduceCycleCount.Store(0)

	// Reset stats for new session
	cc.ResetStats()

	// Transition back to STARTUP
	if err := cc.stateMachine.TransitionTo(StateStartup); err != nil {
		log.Printf("[ContinuousCollector] Failed to transition to STARTUP: %v", err)
		return
	}

	// Seed and start collecting again
	if err := cc.seedAndStartCollecting(ctx); err != nil {
		log.Printf("[ContinuousCollector] Failed to restart collection: %v", err)
	}

	// Send success notification
	if cc.notifyFunc != nil {
		if err := cc.notifyFunc(ctx, "New session started! Fresh crawl beginning from top of ladder."); err != nil {
			log.Printf("[ContinuousCollector] Failed to send success notification: %v", err)
		}
	}
}

// initiateShutdown triggers a graceful shutdown
func (cc *ContinuousCollector) initiateShutdown(ctx context.Context) {
	cc.shutdownOnce.Do(func() {
		log.Println("[ContinuousCollector] Initiating graceful shutdown...")

		// Trigger final reduce if we have data
		if cc.stateMachine.CanReduce() {
			cc.triggerReduce()

			// Wait for reduce to complete with timeout
			shutdownCtx, cancel := context.WithTimeout(ctx, cc.config.ShutdownTimeout)
			defer cancel()

			// Wait for state to reach PUSHING or beyond
		waitLoop:
			for cc.stateMachine.Current() == StateReducing {
				select {
				case <-shutdownCtx.Done():
					log.Println("[ContinuousCollector] Shutdown timeout during reduce")
					break waitLoop
				default:
					time.Sleep(100 * time.Millisecond)
				}
			}
		}

		// Transition to SHUTDOWN
		cc.stateMachine.TransitionTo(StateShutdown)

		// Signal shutdown
		close(cc.shutdownCh)
	})
}

// Shutdown triggers a graceful shutdown from external code
func (cc *ContinuousCollector) Shutdown(ctx context.Context) {
	cc.initiateShutdown(ctx)
	cc.wg.Wait()
}

// State returns the current state machine state
func (cc *ContinuousCollector) State() State {
	return cc.stateMachine.Current()
}

// GetStateMachine returns the internal state machine (for testing)
func (cc *ContinuousCollector) GetStateMachine() *StateMachine {
	return cc.stateMachine
}

// GetWarmLock returns the warm lock (for testing)
func (cc *ContinuousCollector) GetWarmLock() *WarmLock {
	return cc.warmLock
}

// GetWarmFileCounter returns the warm file counter (for testing)
func (cc *ContinuousCollector) GetWarmFileCounter() *WarmFileCounter {
	return cc.warmFileCounter
}

// IncrementWarmFileCount manually increments the warm file counter (called by rotator)
func (cc *ContinuousCollector) IncrementWarmFileCount() {
	cc.warmFileCounter.Increment()
}

// onStateTransition is called on each state transition
func (cc *ContinuousCollector) onStateTransition(from, to State) {
	log.Printf("[ContinuousCollector] State transition: %s → %s", from, to)
}

// CollectorStats contains statistics about the collection run
type CollectorStats struct {
	MatchesCollected int64
	RuntimeSeconds   int64
	LastReduceAgo    int64 // seconds since last reduce, -1 if never reduced
}

// GetStats returns current collection statistics
func (cc *ContinuousCollector) GetStats() CollectorStats {
	stats := CollectorStats{
		MatchesCollected: cc.matchesCollected.Load(),
		RuntimeSeconds:   int64(time.Since(cc.startTime).Seconds()),
		LastReduceAgo:    -1,
	}

	if lastReduce := cc.lastReduceTime.Load(); lastReduce != nil {
		if t, ok := lastReduce.(time.Time); ok && !t.IsZero() {
			stats.LastReduceAgo = int64(time.Since(t).Seconds())
		}
	}

	return stats
}

// IncrementMatchCount adds to the total match count (called by spider)
func (cc *ContinuousCollector) IncrementMatchCount(count int64) {
	cc.matchesCollected.Add(count)
}

// ResetStats resets the statistics for a fresh session
func (cc *ContinuousCollector) ResetStats() {
	cc.matchesCollected.Store(0)
	cc.startTime = time.Now()
	cc.lastReduceTime.Store(time.Time{})
}
