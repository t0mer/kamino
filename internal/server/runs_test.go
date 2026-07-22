package server_test

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/t0mer/kamino/internal/download"
	"github.com/t0mer/kamino/internal/events"
	kexec "github.com/t0mer/kamino/internal/exec"
	"github.com/t0mer/kamino/internal/runmgr"
	"github.com/t0mer/kamino/internal/server"
	"github.com/t0mer/kamino/internal/state"
)

// newRunServer builds a server wired with a kexec.FakeExecutor and a
// download.FakeDownloader (via server.Deps.Exec/Download), so a POST /runs
// against the returned handler resolves entirely in memory. Without this, a
// test that reaches runmgr.Start would run through runmgr's production
// default — kexec.NewRealExecutor() — and genuinely shell out to apt-get as
// root, against whatever the plan's items happen to be. The config repo the
// plan is built from is arbitrary, untrusted input in production; a test
// fixture is no different in kind, so no test in this package may be allowed
// to reach the real executor.
func newRunServer(t *testing.T) (http.Handler, *state.Store, *runmgr.Manager) {
	t.Helper()
	db, err := state.Open(filepath.Join(t.TempDir(), "k.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	bus := events.NewBus(events.DefaultBuffer)
	t.Cleanup(bus.Close)

	mgr := runmgr.New(db, bus, 50)
	h := server.New(server.Deps{
		DB:         db,
		Bus:        bus,
		Runs:       mgr,
		DataDir:    t.TempDir(),
		APIToken:   testToken,
		LoadConfig: loadFixtureConfig(t),
		Exec:       kexec.NewFakeExecutor(),
		Download:   download.NewFakeDownloader(),
	}).Handler()
	return h, db, mgr
}

// waitIdle blocks until mgr reports no active run. Any test that drives a
// POST /runs past validation must call this before returning: the run
// executes on its own goroutine, and a still-running goroutine writing to
// *state.Store after t.Cleanup closes it (see newRunServer) would spam
// unrelated tests with "sql: database is closed" warnings.
func waitIdle(t *testing.T, mgr *runmgr.Manager) {
	t.Helper()
	require.Eventually(t, func() bool {
		_, busy := mgr.Active()
		return !busy
	}, 5*time.Second, 5*time.Millisecond, "the run must reach a terminal state before the test ends")
}

func TestListRunsIsEmptyInitially(t *testing.T) {
	h, _, _ := newRunServer(t)

	rec := do(t, h, http.MethodGet, "/api/v1/runs", "")

	require.Equal(t, http.StatusOK, rec.Code)
	var got []any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Empty(t, got)
}

func TestGetUnknownRunIs404(t *testing.T) {
	h, _, _ := newRunServer(t)

	rec := do(t, h, http.MethodGet, "/api/v1/runs/nope", "")

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestCreateRunMissingSecretIs422(t *testing.T) {
	h, db, mgr := newRunServer(t)

	rec := do(t, h, http.MethodPost, "/api/v1/runs", `{"profile":"production","arch":"amd64"}`)

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Contains(t, rec.Body.String(), "CF_TUNNEL_TOKEN",
		"the missing secret must be named, and named before anything is installed")

	// The 422 must be returned before runmgr.Start is ever reached: no run
	// slot was claimed, and nothing was persisted.
	_, busy := mgr.Active()
	assert.False(t, busy, "a missing-secret 422 must not have started a run")
	runs, err := db.ListRuns(50)
	require.NoError(t, err)
	assert.Empty(t, runs, "a missing-secret 422 must not have persisted a run")
}

// TestCreateRunNeverEchoesASecret deliberately targets the "test" profile
// (a single `jq` item, see testdata/config/profiles/test.yaml) rather than
// "production". Unlike TestCreateRunMissingSecretIs422, this test supplies
// every required secret, so validation passes and runmgr.Start actually
// launches a background run — but newRunServer wires it with a
// kexec.FakeExecutor and a download.FakeDownloader, so the run resolves
// entirely in memory: no apt-get, no curl-pipe-sh, no real .deb download,
// regardless of which profile is selected. "production" pulls in heavier
// items (tools/docker, network/cloudflared) that would be a poor fit for a
// unit test even against a fake, so "test" (tools/jq only) is used here; that
// choice is no longer load-bearing for safety the way it was when this test
// ran against the real executor. The supplied secret name (CF_TUNNEL_TOKEN)
// is not even declared by this profile's items — Missing() only checks names
// the plan actually declares — which makes the assertion strictly more
// general: it must hold even for a secret nothing in the plan asked for.
func TestCreateRunNeverEchoesASecret(t *testing.T) {
	h, _, mgr := newRunServer(t)

	rec := do(t, h, http.MethodPost, "/api/v1/runs",
		`{"profile":"test","arch":"amd64","secrets":{"CF_TUNNEL_TOKEN":"sup3rs3cret"}}`)

	assert.NotContains(t, rec.Body.String(), "sup3rs3cret")
	waitIdle(t, mgr)
}

func TestCreateRunUnknownProfileIs404(t *testing.T) {
	h, _, _ := newRunServer(t)

	rec := do(t, h, http.MethodPost, "/api/v1/runs", `{"profile":"nope"}`)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestCreateRunMalformedBodyIs400(t *testing.T) {
	h, _, _ := newRunServer(t)

	rec := do(t, h, http.MethodPost, "/api/v1/runs", "{not json")

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestCancelUnknownRunIs404(t *testing.T) {
	h, _, _ := newRunServer(t)

	rec := do(t, h, http.MethodPost, "/api/v1/runs/nope/cancel", "")

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestGetRunReturnsItsSteps(t *testing.T) {
	h, db, _ := newRunServer(t)

	require.NoError(t, db.CreateRun(state.Run{ID: "run-1", Profile: "dev", Status: state.StatusSuccess}))
	require.NoError(t, db.CreateStep(state.Step{
		ID: "s1", RunID: "run-1", ItemRef: "tools/jq", Name: "jq", Status: state.StatusSuccess,
	}))

	rec := do(t, h, http.MethodGet, "/api/v1/runs/run-1", "")

	require.Equal(t, http.StatusOK, rec.Code)
	var got struct {
		ID    string `json:"id"`
		Steps []struct {
			ItemRef string `json:"item_ref"`
		} `json:"steps"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Equal(t, "run-1", got.ID)
	require.Len(t, got.Steps, 1)
	assert.Equal(t, "tools/jq", got.Steps[0].ItemRef)
}

func TestGetRunIncludesPerStepTimings(t *testing.T) {
	h, db, _ := newRunServer(t)

	start := time.Date(2026, 7, 22, 10, 0, 0, 0, time.UTC)
	fin := start.Add(3 * time.Second)
	require.NoError(t, db.CreateRun(state.Run{ID: "run-1", Profile: "dev", Status: state.StatusSuccess, StartedAt: start}))
	require.NoError(t, db.CreateStep(state.Step{
		ID: "s1", RunID: "run-1", ItemRef: "tools/jq", Name: "jq", Status: state.StatusSuccess,
	}))
	require.NoError(t, db.UpdateStepStatus("s1", state.StatusRunning, start, 0))
	require.NoError(t, db.UpdateStepStatus("s1", state.StatusSuccess, fin, 0))

	rec := do(t, h, http.MethodGet, "/api/v1/runs/run-1", "")

	require.Equal(t, http.StatusOK, rec.Code)
	var got struct {
		Steps []struct {
			StartedAt  string `json:"started_at"`
			FinishedAt string `json:"finished_at"`
		} `json:"steps"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Len(t, got.Steps, 1)
	assert.Equal(t, "2026-07-22T10:00:00Z", got.Steps[0].StartedAt,
		"the UI needs per-step timings to show durations for a completed run")
	assert.Equal(t, "2026-07-22T10:00:03Z", got.Steps[0].FinishedAt)
}
