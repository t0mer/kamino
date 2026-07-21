package engine

import "github.com/t0mer/kamino/internal/state"

// Sink receives run progress. Implementations must be cheap and must never
// block the engine: a slow sink would stall an install.
type Sink interface {
	StepStatus(runID, stepID string, s state.Status)
	LogLine(runID, stepID, stream, line string)
}

// MultiSink fans out to several sinks.
type MultiSink struct {
	sinks []Sink
}

// NewMultiSink combines sinks.
func NewMultiSink(sinks ...Sink) *MultiSink { return &MultiSink{sinks: sinks} }

// StepStatus forwards a status transition to every sink.
func (m *MultiSink) StepStatus(runID, stepID string, s state.Status) {
	for _, sink := range m.sinks {
		sink.StepStatus(runID, stepID, s)
	}
}

// LogLine forwards a log line to every sink.
func (m *MultiSink) LogLine(runID, stepID, stream, line string) {
	for _, sink := range m.sinks {
		sink.LogLine(runID, stepID, stream, line)
	}
}

// nopSink discards everything. Useful as a default.
type nopSink struct{}

func (nopSink) StepStatus(string, string, state.Status) {}
func (nopSink) LogLine(string, string, string, string)  {}
