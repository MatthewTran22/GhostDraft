package collector

import (
	"sync"
	"sync/atomic"
	"testing"
)

// 8.1 Unit: Increment counter atomically
func TestWarmFileCounter_IncrementAtomic(t *testing.T) {
	counter := NewWarmFileCounter(100, nil) // high threshold so callback doesn't fire

	// Run 1000 concurrent increments
	var wg sync.WaitGroup
	numGoroutines := 100
	incrementsPerGoroutine := 10

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < incrementsPerGoroutine; j++ {
				counter.Increment()
			}
		}()
	}

	wg.Wait()

	expected := int64(numGoroutines * incrementsPerGoroutine)
	if counter.Count() != expected {
		t.Errorf("expected count %d, got %d", expected, counter.Count())
	}
}

// 8.2 Unit: Trigger callback at threshold
func TestWarmFileCounter_TriggerCallback(t *testing.T) {
	threshold := int64(10)
	var callbackCount atomic.Int32

	callback := func() {
		callbackCount.Add(1)
	}

	counter := NewWarmFileCounter(threshold, callback)

	// Increment to threshold
	for i := int64(0); i < threshold; i++ {
		counter.Increment()
	}

	if callbackCount.Load() != 1 {
		t.Errorf("expected callback to be invoked exactly once, got %d", callbackCount.Load())
	}

	// Continue incrementing - callback should not fire again until reset
	for i := int64(0); i < 5; i++ {
		counter.Increment()
	}

	if callbackCount.Load() != 1 {
		t.Errorf("expected callback to still be 1 after additional increments, got %d", callbackCount.Load())
	}
}

// 8.2 additional: Callback fires exactly at threshold, not before
func TestWarmFileCounter_CallbackAtExactThreshold(t *testing.T) {
	threshold := int64(5)
	var callbackCount atomic.Int32
	var callbackAtCount int64

	callback := func() {
		callbackCount.Add(1)
	}

	counter := NewWarmFileCounter(threshold, callback)

	// Increment one less than threshold
	for i := int64(0); i < threshold-1; i++ {
		counter.Increment()
		callbackAtCount = counter.Count()
	}

	if callbackCount.Load() != 0 {
		t.Errorf("callback should not fire before threshold, count was %d", callbackAtCount)
	}

	// One more increment should trigger
	counter.Increment()

	if callbackCount.Load() != 1 {
		t.Errorf("callback should fire at threshold")
	}
}

// 8.3 Unit: Reset counter
func TestWarmFileCounter_Reset(t *testing.T) {
	threshold := int64(10)
	var callbackCount atomic.Int32

	callback := func() {
		callbackCount.Add(1)
	}

	counter := NewWarmFileCounter(threshold, callback)

	// Increment to 5
	for i := 0; i < 5; i++ {
		counter.Increment()
	}

	if counter.Count() != 5 {
		t.Errorf("expected count 5, got %d", counter.Count())
	}

	// Reset
	counter.Reset()

	if counter.Count() != 0 {
		t.Errorf("expected count 0 after reset, got %d", counter.Count())
	}

	// Increment again to threshold - callback should fire
	for i := int64(0); i < threshold; i++ {
		counter.Increment()
	}

	if callbackCount.Load() != 1 {
		t.Errorf("callback should fire after reset and reaching threshold, got %d", callbackCount.Load())
	}
}

// 8.3 additional: Reset allows callback to fire again
func TestWarmFileCounter_ResetAllowsNewCallback(t *testing.T) {
	threshold := int64(3)
	var callbackCount atomic.Int32

	callback := func() {
		callbackCount.Add(1)
	}

	counter := NewWarmFileCounter(threshold, callback)

	// First cycle
	for i := int64(0); i < threshold; i++ {
		counter.Increment()
	}
	if callbackCount.Load() != 1 {
		t.Errorf("first callback not fired")
	}

	// Reset and second cycle
	counter.Reset()
	for i := int64(0); i < threshold; i++ {
		counter.Increment()
	}
	if callbackCount.Load() != 2 {
		t.Errorf("expected 2 callbacks after reset cycle, got %d", callbackCount.Load())
	}

	// Reset and third cycle
	counter.Reset()
	for i := int64(0); i < threshold; i++ {
		counter.Increment()
	}
	if callbackCount.Load() != 3 {
		t.Errorf("expected 3 callbacks after second reset cycle, got %d", callbackCount.Load())
	}
}

// Test nil callback doesn't panic
func TestWarmFileCounter_NilCallback(t *testing.T) {
	counter := NewWarmFileCounter(3, nil)

	// Should not panic
	for i := 0; i < 10; i++ {
		counter.Increment()
	}

	if counter.Count() != 10 {
		t.Errorf("expected count 10, got %d", counter.Count())
	}
}

// Test threshold of 1
func TestWarmFileCounter_ThresholdOne(t *testing.T) {
	var callbackCount atomic.Int32

	callback := func() {
		callbackCount.Add(1)
	}

	counter := NewWarmFileCounter(1, callback)

	counter.Increment()

	if callbackCount.Load() != 1 {
		t.Errorf("callback should fire on first increment with threshold 1")
	}
}
