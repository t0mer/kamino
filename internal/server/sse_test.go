package server_test

import (
	"bufio"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/t0mer/kamino/internal/events"
	"github.com/t0mer/kamino/internal/runmgr"
	"github.com/t0mer/kamino/internal/server"
	"github.com/t0mer/kamino/internal/state"
)

func newSSEServer(t *testing.T) (*httptest.Server, *state.Store, *events.Bus) {
	t.Helper()
	db, err := state.Open(filepath.Join(t.TempDir(), "k.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	bus := events.NewBus(events.DefaultBuffer)
	t.Cleanup(bus.Close)

	h := server.New(server.Deps{
		DB: db, Bus: bus, Runs: runmgr.New(db, bus, 50),
		DataDir: t.TempDir(), APIToken: testToken,
	}).Handler()

	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv, db, bus
}

func TestEventsReplaysAFinishedRunAndCloses(t *testing.T) {
	srv, db, _ := newSSEServer(t)

	now := time.Now().UTC()
	require.NoError(t, db.CreateRun(state.Run{
		ID: "run-1", Profile: "dev", Status: state.StatusSuccess, StartedAt: now,
	}))
	require.NoError(t, db.CreateStep(state.Step{
		ID: "s1", RunID: "run-1", ItemRef: "tools/jq", Name: "jq", Status: state.StatusSuccess,
	}))
	require.NoError(t, db.AppendLog("s1", now, "stdout", "installing jq"))
	require.NoError(t, db.FinishRun("run-1", state.StatusSuccess, now))

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/runs/run-1/events", nil)
	require.NoError(t, err)
	req.Header.Set(server.TokenHeader, testToken)

	resp, err := srv.Client().Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))

	body := readSSE(t, resp)
	assert.Contains(t, body, "installing jq", "a finished run must replay its logs")
	assert.Contains(t, body, "tools/jq")
}

func TestEventsRejectsAnUnknownRun(t *testing.T) {
	srv, _, _ := newSSEServer(t)

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/runs/nope/events", nil)
	require.NoError(t, err)
	req.Header.Set(server.TokenHeader, testToken)

	resp, err := srv.Client().Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestEventsRequiresAToken(t *testing.T) {
	srv, db, _ := newSSEServer(t)
	require.NoError(t, db.CreateRun(state.Run{ID: "run-1", Status: state.StatusSuccess}))

	resp, err := srv.Client().Get(srv.URL + "/api/v1/runs/run-1/events")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestEventsStreamsLiveEventsAfterReplay(t *testing.T) {
	srv, db, bus := newSSEServer(t)

	now := time.Now().UTC()
	require.NoError(t, db.CreateRun(state.Run{
		ID: "run-1", Profile: "dev", Status: state.StatusRunning, StartedAt: now,
	}))
	require.NoError(t, db.CreateStep(state.Step{
		ID: "s1", RunID: "run-1", ItemRef: "tools/jq", Name: "jq", Status: state.StatusRunning,
	}))
	require.NoError(t, db.AppendLog("s1", now, "stdout", "replayed line"))

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/runs/run-1/events", nil)
	require.NoError(t, err)
	req.Header.Set(server.TokenHeader, testToken)

	resp, err := srv.Client().Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	// Give the handler time to finish replaying and subscribe, then publish a
	// terminal run event so the stream closes and the test can read it whole.
	go func() {
		time.Sleep(200 * time.Millisecond)
		bus.Publish(events.Event{Type: events.EventLog, RunID: "run-1", StepID: "s1", Line: "live line"})
		time.Sleep(50 * time.Millisecond)
		bus.Publish(events.Event{Type: events.EventRun, RunID: "run-1", Status: string(state.StatusSuccess)})
	}()

	body := readSSE(t, resp)

	replayAt := strings.Index(body, "replayed line")
	liveAt := strings.Index(body, "live line")
	require.NotEqual(t, -1, replayAt, "the persisted line must be replayed")
	require.NotEqual(t, -1, liveAt, "the live line must follow")
	assert.Less(t, replayAt, liveAt, "replay must precede live events")
}

// TestEventsReplayUsesItemRefNotUUID pins the wire contract: the replay path
// reads steps from sqlite keyed by an opaque uuid (state.Step.ID), but the
// live bus speaks in item refs (state.Step.ItemRef) because that is the
// plan's own vocabulary. If replay emitted the uuid, a browser could never
// correlate a replayed step/log with the same step's live events, which use
// the ref. The replayed event's step_id must be the ref, never the uuid.
func TestEventsReplayUsesItemRefNotUUID(t *testing.T) {
	srv, db, _ := newSSEServer(t)

	now := time.Now().UTC()
	require.NoError(t, db.CreateRun(state.Run{
		ID: "run-1", Profile: "dev", Status: state.StatusSuccess, StartedAt: now,
	}))
	require.NoError(t, db.CreateStep(state.Step{
		ID: "uuid-should-not-appear-on-wire", RunID: "run-1", ItemRef: "tools/jq", Name: "jq", Status: state.StatusSuccess,
	}))
	require.NoError(t, db.AppendLog("uuid-should-not-appear-on-wire", now, "stdout", "installing jq"))
	require.NoError(t, db.FinishRun("run-1", state.StatusSuccess, now))

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/runs/run-1/events", nil)
	require.NoError(t, err)
	req.Header.Set(server.TokenHeader, testToken)

	resp, err := srv.Client().Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	body := readSSE(t, resp)

	assert.Contains(t, body, `"step_id":"tools/jq"`, "replayed events must carry the item ref as step_id")
	assert.NotContains(t, body, "uuid-should-not-appear-on-wire", "replayed events must never leak the internal uuid")
}

// readSSE drains an event stream until it closes or a timeout elapses.
func readSSE(t *testing.T, resp *http.Response) string {
	t.Helper()
	var sb strings.Builder
	done := make(chan struct{})

	go func() {
		defer close(done)
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			sb.WriteString(sc.Text())
			sb.WriteString("\n")
		}
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		_ = resp.Body.Close()
		<-done
	}
	return sb.String()
}

// TestEventsDoesNotReplayThenAlsoDeliverTheSameLine pins the replay watermark.
//
// A step transition and log line are persisted (so replay renders them) AND an
// event bearing an old timestamp is published live for the same content. The
// stream must show that line exactly once: an event at or before the replay
// watermark is already covered by the replay and must be dropped live, or the
// UI double-prints. A genuinely newer live line must still get through.
func TestEventsDoesNotReplayThenAlsoDeliverTheSameLine(t *testing.T) {
	srv, db, bus := newSSEServer(t)

	now := time.Now().UTC()
	require.NoError(t, db.CreateRun(state.Run{
		ID: "run-1", Profile: "dev", Status: state.StatusRunning, StartedAt: now,
	}))
	require.NoError(t, db.CreateStep(state.Step{
		ID: "s1", RunID: "run-1", ItemRef: "tools/jq", Name: "jq", Status: state.StatusRunning,
	}))
	require.NoError(t, db.AppendLog("s1", now, "stdout", "persisted line"))

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/runs/run-1/events", nil)
	require.NoError(t, err)
	req.Header.Set(server.TokenHeader, testToken)

	resp, err := srv.Client().Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	go func() {
		time.Sleep(200 * time.Millisecond)
		// A stale duplicate: same content, timestamp from before the stream
		// connected, i.e. at or before the replay watermark. Must be dropped.
		bus.Publish(events.Event{
			Type: events.EventLog, RunID: "run-1", StepID: "tools/jq",
			Stream: "stdout", Line: "persisted line", TS: now,
		})
		// A genuinely new line, stamped now. Must get through.
		bus.Publish(events.Event{
			Type: events.EventLog, RunID: "run-1", StepID: "tools/jq",
			Stream: "stdout", Line: "fresh line", TS: time.Now().UTC().Add(time.Hour),
		})
		time.Sleep(50 * time.Millisecond)
		bus.Publish(events.Event{Type: events.EventRun, RunID: "run-1", Status: string(state.StatusSuccess)})
	}()

	body := readSSE(t, resp)

	assert.Equal(t, 1, strings.Count(body, "persisted line"),
		"a line that was replayed must not also be delivered live")
	assert.Contains(t, body, "fresh line", "a genuinely new live line must still get through")
}
