package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/t0mer/kamino/internal/state"
)

// TestTermSinkCounterNeverExceedsTotal pins the progress counter.
//
// The engine announces a step as running and then reports its outcome, so a
// skipped step arrives as two transitions. Counting both printed "[2/1]" on a
// one-step plan — the first thing an operator sees, and it looks broken.
func TestTermSinkCounterNeverExceedsTotal(t *testing.T) {
	tests := []struct {
		name     string
		statuses []state.Status
		total    int
		want     string
	}{
		{
			name:     "skipped step counts once",
			statuses: []state.Status{state.StatusRunning, state.StatusSkipped},
			total:    1,
			want:     "[1/1]",
		},
		{
			name:     "installed step counts once",
			statuses: []state.Status{state.StatusRunning, state.StatusSuccess},
			total:    1,
			want:     "[1/1]",
		},
		{
			name:     "failed step counts once",
			statuses: []state.Status{state.StatusRunning, state.StatusFailed},
			total:    1,
			want:     "[1/1]",
		},
		{
			name:     "blocked step counts once without a running transition",
			statuses: []state.Status{state.StatusBlocked},
			total:    1,
			want:     "[1/1]",
		},
		{
			name:     "cancelled step is reported rather than silently dropped",
			statuses: []state.Status{state.StatusCancelled},
			total:    1,
			want:     "cancelled",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			sink := newTermSink(&out, tc.total, map[string]string{"c/a": "A"}, false)

			for _, s := range tc.statuses {
				sink.StepStatus("run-1", "c/a", s, 0)
			}

			assert.Contains(t, out.String(), tc.want)
			assert.NotContains(t, out.String(), "[2/1]", "the counter must never exceed the total")
		})
	}
}

// TestTermSinkShowsExitCodeOnFailure pins Finding 3 at the terminal-output
// layer: a failed step's exit code, now carried by StepStatus, must reach the
// operator watching the terminal, not just sqlite.
func TestTermSinkShowsExitCodeOnFailure(t *testing.T) {
	var out bytes.Buffer
	sink := newTermSink(&out, 1, map[string]string{"c/a": "A"}, false)

	sink.StepStatus("run-1", "c/a", state.StatusRunning, 0)
	sink.StepStatus("run-1", "c/a", state.StatusFailed, 42)

	assert.Contains(t, out.String(), "exit 42")
}

func TestTermSinkCountsEachStepOfAMultiStepPlan(t *testing.T) {
	var out bytes.Buffer
	sink := newTermSink(&out, 3, map[string]string{"c/a": "A", "c/b": "B", "c/c": "C"}, false)

	sink.StepStatus("run-1", "c/a", state.StatusRunning, 0)
	sink.StepStatus("run-1", "c/a", state.StatusSuccess, 0)
	sink.StepStatus("run-1", "c/b", state.StatusRunning, 0)
	sink.StepStatus("run-1", "c/b", state.StatusSkipped, 0)
	sink.StepStatus("run-1", "c/c", state.StatusBlocked, 0)

	got := out.String()
	assert.Contains(t, got, "[1/3]")
	assert.Contains(t, got, "[2/3]")
	assert.Contains(t, got, "[3/3]")
	assert.NotContains(t, got, "[4/3]")
}
