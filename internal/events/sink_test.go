package events_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/t0mer/kamino/internal/engine"
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

// TestSinkIsAcceptedByTheEngineMultiSink proves the adapter satisfies the real
// engine.Sink interface, not merely the shape asserted inline in sink.go.
//
// This test lives in package events_test rather than events precisely so it may
// import engine: the production package must never do so, since engine imports
// events and the reverse would be an import cycle. Constructing a MultiSink
// around this sink is the only way to prove the two agree.
func TestSinkIsAcceptedByTheEngineMultiSink(t *testing.T) {
	bus := events.NewBus(events.DefaultBuffer)
	defer bus.Close()

	ch, unsub := bus.Subscribe("run-1")
	defer unsub()

	// If events.Sink ever drifts from engine.Sink, this line stops compiling.
	multi := engine.NewMultiSink(events.NewSink(bus))
	multi.StepStatus("run-1", "tools/jq", state.StatusSuccess, 0)

	got := <-ch
	assert.Equal(t, events.EventStep, got.Type)
	assert.Equal(t, string(state.StatusSuccess), got.Status,
		"a status routed through the engine's own fan-out must reach the bus")
}
