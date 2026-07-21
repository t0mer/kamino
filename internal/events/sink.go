package events

import "github.com/t0mer/kamino/internal/state"

// Sink publishes engine progress onto a Bus. It satisfies engine.Sink, so it
// slots into the same MultiSink that already carries progress to sqlite and
// the terminal.
//
// Every line reaching this sink has already been redacted by the engine (see
// internal/engine.Run's single redaction chokepoint). This type must not add a
// path around that: anything it publishes is going straight to a browser.
type Sink struct {
	bus *Bus
}

// NewSink builds a sink publishing to bus.
func NewSink(bus *Bus) *Sink { return &Sink{bus: bus} }

// StepStatus publishes a step transition.
func (s *Sink) StepStatus(runID, stepID string, status state.Status, exitCode int) {
	s.bus.Publish(Event{
		Type:   EventStep,
		RunID:  runID,
		StepID: stepID,
		Status: string(status),
	})
}

// LogLine publishes one captured output line.
func (s *Sink) LogLine(runID, stepID, stream, line string) {
	s.bus.Publish(Event{
		Type:   EventLog,
		RunID:  runID,
		StepID: stepID,
		Stream: stream,
		Line:   line,
	})
}

// Compile-time proof that Sink satisfies the engine's sink contract. The
// interface is declared in internal/engine; asserting it by shape here keeps
// events a leaf package rather than importing engine back.
var _ interface {
	StepStatus(runID, stepID string, status state.Status, exitCode int)
	LogLine(runID, stepID, stream, line string)
} = (*Sink)(nil)
