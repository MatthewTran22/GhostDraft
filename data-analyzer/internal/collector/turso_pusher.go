package collector

import (
	"context"
	"sync"
)

// DataPusher is an interface for pushing aggregated data to a data store
type DataPusher interface {
	PushAggData(ctx context.Context, data *AggData) error
}

// TursoPusher handles asynchronous, sequential pushes to Turso
type TursoPusher struct {
	pusher   DataPusher
	pushChan chan *AggData
	wg       sync.WaitGroup
	started  bool
	mu       sync.Mutex
}

// NewTursoPusher creates a new TursoPusher with default buffer size
func NewTursoPusher(pusher DataPusher) *TursoPusher {
	return NewTursoPusherWithBuffer(pusher, 10)
}

// NewTursoPusherWithBuffer creates a new TursoPusher with specified buffer size
func NewTursoPusherWithBuffer(pusher DataPusher, bufferSize int) *TursoPusher {
	return &TursoPusher{
		pusher:   pusher,
		pushChan: make(chan *AggData, bufferSize),
	}
}

// Start begins processing pushes in a background goroutine
func (t *TursoPusher) Start(ctx context.Context) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.started {
		return
	}
	t.started = true

	t.wg.Add(1)
	go t.processLoop(ctx)
}

// processLoop reads from the channel and processes pushes sequentially
func (t *TursoPusher) processLoop(ctx context.Context) {
	defer t.wg.Done()

	for {
		select {
		case data, ok := <-t.pushChan:
			if !ok {
				// Channel closed, drain any remaining items
				return
			}
			// Process the push (blocking, sequential)
			// We use a background context here to ensure pushes complete
			// even if the parent context is cancelled
			_ = t.pusher.PushAggData(context.Background(), data)

		case <-ctx.Done():
			// Context cancelled, but drain remaining items
			t.drainChannel()
			return
		}
	}
}

// drainChannel processes any remaining items in the channel
func (t *TursoPusher) drainChannel() {
	for {
		select {
		case data, ok := <-t.pushChan:
			if !ok {
				return
			}
			_ = t.pusher.PushAggData(context.Background(), data)
		default:
			return
		}
	}
}

// Push sends data to the push queue. Blocks if the queue is full.
func (t *TursoPusher) Push(ctx context.Context, data *AggData) error {
	select {
	case t.pushChan <- data:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Wait blocks until all pending pushes are complete
func (t *TursoPusher) Wait() {
	t.mu.Lock()
	if !t.started {
		t.mu.Unlock()
		return
	}
	t.mu.Unlock()

	// Close channel to signal no more pushes
	close(t.pushChan)

	// Wait for processing to complete
	t.wg.Wait()
}

// PendingCount returns the number of pushes waiting in the queue
func (t *TursoPusher) PendingCount() int {
	return len(t.pushChan)
}
