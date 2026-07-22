package server_test

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/t0mer/kamino/internal/events"
	"github.com/t0mer/kamino/internal/runmgr"
	"github.com/t0mer/kamino/internal/server"
	"github.com/t0mer/kamino/internal/state"
)

func newRunServer(t *testing.T) (http.Handler, *state.Store) {
	t.Helper()
	db, err := state.Open(filepath.Join(t.TempDir(), "k.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	bus := events.NewBus(events.DefaultBuffer)
	t.Cleanup(bus.Close)

	h := server.New(server.Deps{
		DB:         db,
		Bus:        bus,
		Runs:       runmgr.New(db, bus, 50),
		DataDir:    t.TempDir(),
		APIToken:   testToken,
		LoadConfig: loadFixtureConfig(t),
	}).Handler()
	return h, db
}

func TestListRunsIsEmptyInitially(t *testing.T) {
	h, _ := newRunServer(t)

	rec := do(t, h, http.MethodGet, "/api/v1/runs", "")

	require.Equal(t, http.StatusOK, rec.Code)
	var got []any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Empty(t, got)
}

func TestGetUnknownRunIs404(t *testing.T) {
	h, _ := newRunServer(t)

	rec := do(t, h, http.MethodGet, "/api/v1/runs/nope", "")

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestCreateRunMissingSecretIs422(t *testing.T) {
	h, _ := newRunServer(t)

	rec := do(t, h, http.MethodPost, "/api/v1/runs", `{"profile":"production","arch":"amd64"}`)

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Contains(t, rec.Body.String(), "CF_TUNNEL_TOKEN",
		"the missing secret must be named, and named before anything is installed")
}

// TestCreateRunNeverEchoesASecret deliberately targets the "test" profile
// (a single already-satisfied `jq` check, see testdata/config/profiles/test.yaml)
// rather than "production". Unlike TestCreateRunMissingSecretIs422, this test
// supplies every required secret, so validation passes and runmgr.Start
// actually launches a real background run against the real command executor.
// "production" pulls in tools/docker (a get.docker.com curl-pipe-sh install),
// tools/docker-compose and network/cloudflared (a real .deb download) — real
// installs and system service changes too heavy and intrusive to trigger from
// a unit test. "test" only resolves to tools/jq, whose `check`/`check_contains`
// (see testdata/config/categories/tools.yaml) is satisfied by jq already being
// present on the test host, so the run completes as an idempotency no-op
// without installing anything. The supplied secret name (CF_TUNNEL_TOKEN) is
// not even declared by this profile's items — Missing() only checks names the
// plan actually declares — which makes the assertion strictly more general:
// it must hold even for a secret nothing in the plan asked for.
func TestCreateRunNeverEchoesASecret(t *testing.T) {
	h, _ := newRunServer(t)

	rec := do(t, h, http.MethodPost, "/api/v1/runs",
		`{"profile":"test","arch":"amd64","secrets":{"CF_TUNNEL_TOKEN":"sup3rs3cret"}}`)

	assert.NotContains(t, rec.Body.String(), "sup3rs3cret")
}

func TestCreateRunUnknownProfileIs404(t *testing.T) {
	h, _ := newRunServer(t)

	rec := do(t, h, http.MethodPost, "/api/v1/runs", `{"profile":"nope"}`)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestCreateRunMalformedBodyIs400(t *testing.T) {
	h, _ := newRunServer(t)

	rec := do(t, h, http.MethodPost, "/api/v1/runs", "{not json")

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestCancelUnknownRunIs404(t *testing.T) {
	h, _ := newRunServer(t)

	rec := do(t, h, http.MethodPost, "/api/v1/runs/nope/cancel", "")

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestGetRunReturnsItsSteps(t *testing.T) {
	h, db := newRunServer(t)

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
