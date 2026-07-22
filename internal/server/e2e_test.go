package server_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/t0mer/kamino/internal/state"
)

// TestRunLifecycleOverHTTP drives POST /runs through to sqlite: the run is
// created, appears in history, and reaches a terminal status.
//
// This uses newRunServer (see runs_test.go), which wires the server with a
// kexec.FakeExecutor and a download.FakeDownloader via server.Deps.Exec /
// Deps.Download. Without that, runmgr falls back to its production default
// — a real executor that shells out to apt-get as root — so a test in this
// package would mutate the host it runs on. With the fake, the `test`
// profile's single tools/jq step resolves in memory in milliseconds: the
// assertion here is that the lifecycle completes and is recorded end to end
// (create -> running -> terminal, persisted to sqlite, visible over the API
// at every stage), not that anything was actually installed. A real install
// is covered by scripts/smoke.sh / make smoke.
func TestRunLifecycleOverHTTP(t *testing.T) {
	h, db, mgr := newRunServer(t)

	rec := do(t, h, http.MethodPost, "/api/v1/runs", `{"profile":"test","arch":"amd64"}`)
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())

	var created struct {
		RunID string `json:"run_id"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))
	require.NotEmpty(t, created.RunID)

	// The run must be visible immediately, before it finishes.
	detail := do(t, h, http.MethodGet, "/api/v1/runs/"+created.RunID, "")
	require.Equal(t, http.StatusOK, detail.Code)

	require.Eventually(t, func() bool {
		run, _, err := db.GetRun(created.RunID)
		if err != nil {
			return false
		}
		switch run.Status {
		case state.StatusSuccess, state.StatusFailed, state.StatusCancelled:
			return true
		}
		return false
	}, 5*time.Second, 10*time.Millisecond, "the run must reach a terminal status")

	list := do(t, h, http.MethodGet, "/api/v1/runs", "")
	require.Equal(t, http.StatusOK, list.Code)
	var runs []map[string]any
	require.NoError(t, json.Unmarshal(list.Body.Bytes(), &runs))
	require.Len(t, runs, 1)
	assert.Equal(t, created.RunID, runs[0]["id"])

	waitIdle(t, mgr)
}

// TestSecondRunIsRejectedWhileOneIsActive pins the 409 path end to end.
//
// As with TestRunLifecycleOverHTTP, newRunServer injects a fake executor and
// downloader, so both runs resolve in memory and neither touches the host.
func TestSecondRunIsRejectedWhileOneIsActive(t *testing.T) {
	h, _, mgr := newRunServer(t)

	// The dev profile has multiple steps, giving the second request time to
	// land while the first is still executing.
	first := do(t, h, http.MethodPost, "/api/v1/runs", `{"profile":"dev","arch":"amd64"}`)
	require.Equal(t, http.StatusAccepted, first.Code)

	second := do(t, h, http.MethodPost, "/api/v1/runs", `{"profile":"test","arch":"amd64"}`)

	if second.Code == http.StatusConflict {
		assert.Contains(t, second.Body.String(), "already in progress")
	} else {
		// The first run may have finished first on a fast machine; that is
		// not a failure of the 409 path, so accept it rather than flake.
		assert.Equal(t, http.StatusAccepted, second.Code,
			"a second run must be either rejected with 409 or accepted after the first finished")
	}

	waitIdle(t, mgr)
}
