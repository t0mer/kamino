package state

import (
	"log/slog"
	"time"
)

// Sink persists engine progress to sqlite. It satisfies engine.Sink.
//
// Persistence errors are logged, never returned: losing a log line must not
// abort an install that is otherwise succeeding.
type Sink struct {
	store   *Store
	stepIDs map[string]string
}

// NewSink builds a sink. stepIDs maps an item ref to its database step id.
func NewSink(store *Store, stepIDs map[string]string) *Sink {
	return &Sink{store: store, stepIDs: stepIDs}
}

// StepStatus records a step transition and its exit code.
func (s *Sink) StepStatus(_, stepRef string, status Status, exitCode int) {
	id, ok := s.stepIDs[stepRef]
	if !ok {
		return
	}
	if err := s.store.UpdateStepStatus(id, status, time.Now().UTC(), exitCode); err != nil {
		slog.Warn("persisting step status failed", "step", stepRef, "error", err)
	}
}

// LogLine records one captured output line.
func (s *Sink) LogLine(_, stepRef, stream, line string) {
	id, ok := s.stepIDs[stepRef]
	if !ok {
		return
	}
	if err := s.store.AppendLog(id, time.Now().UTC(), stream, line); err != nil {
		slog.Warn("persisting log line failed", "step", stepRef, "error", err)
	}
}
