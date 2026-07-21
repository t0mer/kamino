package events_test

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/t0mer/kamino/internal/events"
)

func TestSubscriberReceivesPublishedEvent(t *testing.T) {
	bus := events.NewBus(events.DefaultBuffer)
	defer bus.Close()

	ch, unsub := bus.Subscribe("run-1")
	defer unsub()

	bus.Publish(events.Event{Type: events.EventLog, RunID: "run-1", Line: "hello"})

	select {
	case got := <-ch:
		assert.Equal(t, "hello", got.Line)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for the event")
	}
}

func TestSubscriberOnlyReceivesItsOwnRun(t *testing.T) {
	bus := events.NewBus(events.DefaultBuffer)
	defer bus.Close()

	ch, unsub := bus.Subscribe("run-1")
	defer unsub()

	bus.Publish(events.Event{Type: events.EventLog, RunID: "run-2", Line: "other"})
	bus.Publish(events.Event{Type: events.EventLog, RunID: "run-1", Line: "mine"})

	got := <-ch
	assert.Equal(t, "mine", got.Line, "an event for another run must not be delivered")
}

func TestPublishDoesNotBlockOnAFullSubscriber(t *testing.T) {
	bus := events.NewBus(2)
	defer bus.Close()

	// Subscribe and never read: the buffer fills immediately.
	_, unsub := bus.Subscribe("run-1")
	defer unsub()

	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			bus.Publish(events.Event{Type: events.EventLog, RunID: "run-1", Line: "flood"})
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Publish blocked on a full subscriber; an install would stall behind a slow browser")
	}
}

func TestUnsubscribeStopsDelivery(t *testing.T) {
	bus := events.NewBus(events.DefaultBuffer)
	defer bus.Close()

	ch, unsub := bus.Subscribe("run-1")
	unsub()

	bus.Publish(events.Event{Type: events.EventLog, RunID: "run-1", Line: "after"})

	_, open := <-ch
	assert.False(t, open, "unsubscribing must close the channel")
}

func TestMultipleSubscribersEachGetEveryEvent(t *testing.T) {
	bus := events.NewBus(events.DefaultBuffer)
	defer bus.Close()

	chA, unsubA := bus.Subscribe("run-1")
	defer unsubA()
	chB, unsubB := bus.Subscribe("run-1")
	defer unsubB()

	bus.Publish(events.Event{Type: events.EventLog, RunID: "run-1", Line: "fanout"})

	assert.Equal(t, "fanout", (<-chA).Line)
	assert.Equal(t, "fanout", (<-chB).Line)
}

func TestConcurrentPublishAndSubscribe(t *testing.T) {
	bus := events.NewBus(events.DefaultBuffer)
	defer bus.Close()

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				bus.Publish(events.Event{Type: events.EventLog, RunID: "run-1", Line: "x"})
			}
		}()
		go func() {
			defer wg.Done()
			ch, unsub := bus.Subscribe("run-1")
			go func() {
				for range ch {
				}
			}()
			time.Sleep(time.Millisecond)
			unsub()
		}()
	}
	wg.Wait()
}

func TestPublishAfterCloseDoesNotPanic(t *testing.T) {
	bus := events.NewBus(events.DefaultBuffer)
	bus.Close()

	assert.NotPanics(t, func() {
		bus.Publish(events.Event{Type: events.EventLog, RunID: "run-1"})
	})
}

func TestTimestampIsSetWhenAbsent(t *testing.T) {
	bus := events.NewBus(events.DefaultBuffer)
	defer bus.Close()

	ch, unsub := bus.Subscribe("run-1")
	defer unsub()

	bus.Publish(events.Event{Type: events.EventLog, RunID: "run-1"})

	got := <-ch
	require.False(t, got.TS.IsZero(), "the bus stamps an event that arrived without a timestamp")
}
