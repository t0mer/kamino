package engine

import "github.com/t0mer/kamino/internal/state"

// Sink receives run progress. Implementations must be cheap and must never
// block the engine: a slow sink would stall an install.
//
// StepStatus carries exitCode alongside every transition rather than through
// a separate call, so a sink cannot record one without the other: the two
// terminal-status persistence writes a sink typically makes (status and exit
// code) stay a single atomic-looking call from the engine's side. exitCode is
// only meaningful for a terminal status reached by actually running a
// command (StatusSuccess, StatusFailed); StepResult leaves it at its zero
// value for every other transition (StatusRunning, StatusSkipped,
// StatusBlocked, StatusCancelled), and callers should treat 0 accordingly.
type Sink interface {
	StepStatus(runID, stepID string, s state.Status, exitCode int)
	LogLine(runID, stepID, stream, line string)
}

// MultiSink fans out to several sinks.
type MultiSink struct {
	sinks []Sink
}

// NewMultiSink combines sinks.
func NewMultiSink(sinks ...Sink) *MultiSink { return &MultiSink{sinks: sinks} }

// StepStatus forwards a status transition, with its exit code, to every sink.
func (m *MultiSink) StepStatus(runID, stepID string, s state.Status, exitCode int) {
	for _, sink := range m.sinks {
		sink.StepStatus(runID, stepID, s, exitCode)
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

func (nopSink) StepStatus(string, string, state.Status, int) {}
func (nopSink) LogLine(string, string, string, string)       {}
