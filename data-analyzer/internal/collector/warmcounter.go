package collector

import (
	"sync"
	"sync/atomic"
)

// WarmFileCounter tracks the number of warm file rotations and triggers
// a callback when a threshold is reached. It is safe for concurrent use.
type WarmFileCounter struct {
	count     atomic.Int64
	threshold int64
	callback  func()
	fired     atomic.Bool // whether callback has fired since last reset
	mu        sync.Mutex  // protects callback invocation
}

// NewWarmFileCounter creates a new counter that triggers the callback
// when count reaches threshold. The callback is invoked exactly once
// per threshold crossing (until Reset is called).
func NewWarmFileCounter(threshold int64, callback func()) *WarmFileCounter {
	return &WarmFileCounter{
		threshold: threshold,
		callback:  callback,
	}
}

// Increment atomically adds 1 to the counter. If the counter reaches
// the threshold and the callback hasn't been fired since the last reset,
// the callback is invoked.
func (c *WarmFileCounter) Increment() {
	newCount := c.count.Add(1)

	// Check if we hit the threshold and haven't fired yet
	if newCount >= c.threshold && !c.fired.Load() {
		c.mu.Lock()
		// Double-check under lock to prevent race
		if !c.fired.Load() {
			c.fired.Store(true)
			if c.callback != nil {
				c.callback()
			}
		}
		c.mu.Unlock()
	}
}

// Count returns the current count value.
func (c *WarmFileCounter) Count() int64 {
	return c.count.Load()
}

// Reset sets the counter back to 0 and allows the callback to fire again.
func (c *WarmFileCounter) Reset() {
	c.mu.Lock()
	c.count.Store(0)
	c.fired.Store(false)
	c.mu.Unlock()
}
