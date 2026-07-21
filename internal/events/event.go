// Package events is an in-process publish/subscribe bus carrying run progress
// from the engine to any number of watching HTTP clients.
//
// It is deliberately a leaf: it imports nothing else in this module, so the
// engine, the run manager and the HTTP layer can all depend on it without
// forming a cycle.
package events

import "time"

// Event types.
const (
	// EventStep reports a step changing status.
	EventStep = "step"
	// EventLog carries one captured output line.
	EventLog = "log"
	// EventRun reports the run itself changing status.
	EventRun = "run"
)

// Event is one thing that happened during a run, as delivered to a watching
// client. The JSON shape is part of the API contract: the web UI decodes it
// directly off the SSE stream.
type Event struct {
	Type   string    `json:"type"`
	RunID  string    `json:"run_id"`
	StepID string    `json:"step_id,omitempty"`
	Status string    `json:"status,omitempty"`
	Stream string    `json:"stream,omitempty"`
	Line   string    `json:"line,omitempty"`
	TS     time.Time `json:"ts"`
}
