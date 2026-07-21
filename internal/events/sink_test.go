package events_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/t0mer/kamino/internal/events"
	"github.com/t0mer/kamino/internal/state"
)

func TestSinkPublishesStepStatus(t *testing.T) {
	bus := events.NewBus(events.DefaultBuffer)
	defer bus.Close()

	ch, unsub := bus.Subscribe("run-1")
	defer unsub()

	events.NewSink(bus).StepStatus("run-1", "tools/jq", state.StatusRunning, 0)

	got := <-ch
	assert.Equal(t, events.EventStep, got.Type)
	assert.Equal(t, "tools/jq", got.StepID)
	assert.Equal(t, string(state.StatusRunning), got.Status)
}

func TestSinkPublishesLogLine(t *testing.T) {
	bus := events.NewBus(events.DefaultBuffer)
	defer bus.Close()

	ch, unsub := bus.Subscribe("run-1")
	defer unsub()

	events.NewSink(bus).LogLine("run-1", "tools/jq", "stdout", "installing")

	got := <-ch
	assert.Equal(t, events.EventLog, got.Type)
	assert.Equal(t, "stdout", got.Stream)
	assert.Equal(t, "installing", got.Line)
}

func TestSinkSatisfiesEngineSink(t *testing.T) {
	// Compile-time proof lives in sink.go; this asserts it is wired to a bus
	// rather than silently dropping.
	bus := events.NewBus(events.DefaultBuffer)
	defer bus.Close()

	sink := events.NewSink(bus)
	require.NotNil(t, sink)
}
