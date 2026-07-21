package events

import (
	"log/slog"
	"sync"
	"time"
)

// DefaultBuffer is how many events are held for a subscriber that is not
// keeping up. Roughly a screenful of log lines: enough to ride out a browser
// repaint, small enough that a dead client cannot hoard memory for a long run.
const DefaultBuffer = 256

// subscriber is one client watching one run.
type subscriber struct {
	runID  string
	ch     chan Event
	lagged bool
}

// Bus fans run events out to subscribers.
//
// Publish never blocks. When a subscriber's buffer is full its events are
// dropped and it is marked lagged; the publisher moves on. That trade is
// deliberate and it is the whole reason this type exists: the publisher is the
// install engine running as root, and a browser that stopped reading must not
// be able to apply backpressure to a dpkg transaction. A dropped log line is a
// far cheaper loss than a wedged install.
type Bus struct {
	buffer int

	mu     sync.Mutex
	subs   map[*subscriber]struct{}
	closed bool
}

// NewBus builds a bus whose subscribers each buffer up to buffer events.
func NewBus(buffer int) *Bus {
	if buffer <= 0 {
		buffer = DefaultBuffer
	}
	return &Bus{buffer: buffer, subs: map[*subscriber]struct{}{}}
}

// Subscribe returns a channel of events for runID and a function that
// unsubscribes and closes it. The caller must always call the returned
// function, or the subscription leaks for the life of the process.
func (b *Bus) Subscribe(runID string) (<-chan Event, func()) {
	s := &subscriber{runID: runID, ch: make(chan Event, b.buffer)}

	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		close(s.ch)
		return s.ch, func() {}
	}
	b.subs[s] = struct{}{}
	b.mu.Unlock()

	var once sync.Once
	return s.ch, func() {
		once.Do(func() {
			b.mu.Lock()
			if _, ok := b.subs[s]; ok {
				delete(b.subs, s)
				close(s.ch)
			}
			b.mu.Unlock()
		})
	}
}

// Publish delivers e to every subscriber watching e.RunID.
func (b *Bus) Publish(e Event) {
	if e.TS.IsZero() {
		e.TS = time.Now().UTC()
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}

	for s := range b.subs {
		if s.runID != e.RunID {
			continue
		}
		select {
		case s.ch <- e:
		default:
			// Full. Drop for this subscriber only and never block the engine.
			if !s.lagged {
				s.lagged = true
				slog.Warn("event subscriber fell behind; dropping events",
					"run_id", e.RunID, "buffer", b.buffer)
			}
		}
	}
}

// Close shuts the bus down and closes every subscriber channel. Publishing
// afterwards is a no-op rather than a panic, so a run finishing after shutdown
// cannot take the process down.
func (b *Bus) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	b.closed = true
	for s := range b.subs {
		delete(b.subs, s)
		close(s.ch)
	}
}
