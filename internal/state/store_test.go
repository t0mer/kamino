package state_test

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/t0mer/kamino/internal/state"
)

func openStore(t *testing.T) *state.Store {
	t.Helper()
	s, err := state.Open(filepath.Join(t.TempDir(), "kamino.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestCreateAndGetRun(t *testing.T) {
	s := openStore(t)
	start := time.Now().UTC().Truncate(time.Second)

	require.NoError(t, s.CreateRun(state.Run{
		ID: "run-1", Profile: "dev", ConfigSHA: "abc123",
		Status: state.StatusRunning, StartedAt: start,
	}))

	got, steps, err := s.GetRun("run-1")

	require.NoError(t, err)
	assert.Equal(t, "dev", got.Profile)
	assert.Equal(t, "abc123", got.ConfigSHA)
	assert.Equal(t, state.StatusRunning, got.Status)
	assert.Empty(t, steps)
}

func TestFinishRunRecordsStatusAndTime(t *testing.T) {
	s := openStore(t)
	start := time.Now().UTC()
	require.NoError(t, s.CreateRun(state.Run{ID: "run-1", Profile: "dev", Status: state.StatusRunning, StartedAt: start}))

	require.NoError(t, s.FinishRun("run-1", state.StatusSuccess, start.Add(time.Minute)))
	got, _, err := s.GetRun("run-1")

	require.NoError(t, err)
	assert.Equal(t, state.StatusSuccess, got.Status)
	require.NotNil(t, got.FinishedAt)
}

func TestStepLifecycle(t *testing.T) {
	s := openStore(t)
	now := time.Now().UTC()
	require.NoError(t, s.CreateRun(state.Run{ID: "run-1", Status: state.StatusRunning, StartedAt: now}))
	require.NoError(t, s.CreateStep(state.Step{
		ID: "step-1", RunID: "run-1", ItemRef: "tools/jq", Name: "jq", Status: state.StatusPending,
	}))

	require.NoError(t, s.UpdateStepStatus("step-1", state.StatusSuccess, now.Add(time.Second), 0))
	_, steps, err := s.GetRun("run-1")

	require.NoError(t, err)
	require.Len(t, steps, 1)
	assert.Equal(t, "tools/jq", steps[0].ItemRef)
	assert.Equal(t, state.StatusSuccess, steps[0].Status)
	require.NotNil(t, steps[0].FinishedAt)
}

func TestStepsComeBackInInsertionOrder(t *testing.T) {
	s := openStore(t)
	now := time.Now().UTC()
	require.NoError(t, s.CreateRun(state.Run{ID: "run-1", Status: state.StatusRunning, StartedAt: now}))
	for i, ref := range []string{"c/a", "c/b", "c/c"} {
		require.NoError(t, s.CreateStep(state.Step{
			ID: fmt.Sprintf("step-%d", i), RunID: "run-1", ItemRef: ref, Status: state.StatusPending,
		}))
	}

	_, steps, err := s.GetRun("run-1")

	require.NoError(t, err)
	assert.Equal(t, []string{"c/a", "c/b", "c/c"},
		[]string{steps[0].ItemRef, steps[1].ItemRef, steps[2].ItemRef})
}

func TestAppendAndReadLogs(t *testing.T) {
	s := openStore(t)
	now := time.Now().UTC()
	require.NoError(t, s.CreateRun(state.Run{ID: "run-1", Status: state.StatusRunning, StartedAt: now}))
	require.NoError(t, s.CreateStep(state.Step{ID: "step-1", RunID: "run-1", ItemRef: "c/a", Status: state.StatusRunning}))

	require.NoError(t, s.AppendLog("step-1", now, "stdout", "line one"))
	require.NoError(t, s.AppendLog("step-1", now.Add(time.Millisecond), "stderr", "line two"))
	got, err := s.StepLogs("step-1")

	require.NoError(t, err)
	assert.Equal(t, []string{"line one", "line two"}, got)
}

func TestListRunsIsNewestFirst(t *testing.T) {
	s := openStore(t)
	base := time.Now().UTC()
	for i := 0; i < 3; i++ {
		require.NoError(t, s.CreateRun(state.Run{
			ID:      fmt.Sprintf("run-%d", i),
			Status:  state.StatusSuccess,
			StartedAt: base.Add(time.Duration(i) * time.Minute),
		}))
	}

	got, err := s.ListRuns(10)

	require.NoError(t, err)
	require.Len(t, got, 3)
	assert.Equal(t, "run-2", got[0].ID, "newest run first")
}

func TestListRunsRespectsLimit(t *testing.T) {
	s := openStore(t)
	base := time.Now().UTC()
	for i := 0; i < 5; i++ {
		require.NoError(t, s.CreateRun(state.Run{
			ID: fmt.Sprintf("run-%d", i), Status: state.StatusSuccess,
			StartedAt: base.Add(time.Duration(i) * time.Minute),
		}))
	}

	got, err := s.ListRuns(2)

	require.NoError(t, err)
	assert.Len(t, got, 2)
}

func TestPruneKeepsNewestRunsAndCascades(t *testing.T) {
	s := openStore(t)
	base := time.Now().UTC()
	for i := 0; i < 5; i++ {
		id := fmt.Sprintf("run-%d", i)
		require.NoError(t, s.CreateRun(state.Run{ID: id, Status: state.StatusSuccess,
			StartedAt: base.Add(time.Duration(i) * time.Minute)}))
		require.NoError(t, s.CreateStep(state.Step{
			ID: "step-" + id, RunID: id, ItemRef: "c/a", Status: state.StatusSuccess,
		}))
		require.NoError(t, s.AppendLog("step-"+id, base, "stdout", "hello"))
	}

	require.NoError(t, s.Prune(2))
	runs, err := s.ListRuns(10)

	require.NoError(t, err)
	require.Len(t, runs, 2)
	assert.Equal(t, "run-4", runs[0].ID)

	logs, err := s.StepLogs("step-run-0")
	require.NoError(t, err)
	assert.Empty(t, logs, "pruning a run must remove its steps and logs too")
}

func TestGetRunUnknownIDIsAnError(t *testing.T) {
	s := openStore(t)

	_, _, err := s.GetRun("nope")

	require.Error(t, err)
}

func TestOpenCreatesParentDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "dir", "kamino.db")

	s, err := state.Open(path)

	require.NoError(t, err)
	require.NoError(t, s.Close())
	assert.FileExists(t, path)
}
