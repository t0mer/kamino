package state_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/t0mer/kamino/internal/state"
)

func TestSinkPersistsStatusAndLogs(t *testing.T) {
	s, err := state.Open(filepath.Join(t.TempDir(), "k.db"))
	require.NoError(t, err)
	defer func() { _ = s.Close() }()

	now := time.Now().UTC()
	require.NoError(t, s.CreateRun(state.Run{ID: "run-1", Status: state.StatusRunning, StartedAt: now}))
	require.NoError(t, s.CreateStep(state.Step{
		ID: "step-1", RunID: "run-1", ItemRef: "tools/jq", Status: state.StatusPending,
	}))

	sink := state.NewSink(s, map[string]string{"tools/jq": "step-1"})
	sink.StepStatus("run-1", "tools/jq", state.StatusRunning, 0)
	sink.LogLine("run-1", "tools/jq", "stdout", "installing")
	sink.StepStatus("run-1", "tools/jq", state.StatusFailed, 42)

	_, steps, err := s.GetRun("run-1")
	require.NoError(t, err)
	assert.Equal(t, state.StatusFailed, steps[0].Status)
	assert.Equal(t, 42, steps[0].ExitCode,
		"the exit code passed to StepStatus must reach sqlite, not the previously hardcoded 0")

	logs, err := s.StepLogs("step-1")
	require.NoError(t, err)
	assert.Equal(t, []string{"installing"}, logs)
}

func TestSinkIgnoresUnknownStepRef(t *testing.T) {
	s, err := state.Open(filepath.Join(t.TempDir(), "k.db"))
	require.NoError(t, err)
	defer func() { _ = s.Close() }()

	sink := state.NewSink(s, map[string]string{})

	assert.NotPanics(t, func() {
		sink.StepStatus("run-1", "unknown/ref", state.StatusRunning, 0)
		sink.LogLine("run-1", "unknown/ref", "stdout", "orphan line")
	}, "a sink must never take down a run in progress")
}
