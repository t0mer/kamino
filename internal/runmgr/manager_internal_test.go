package runmgr

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/t0mer/kamino/internal/download"
	"github.com/t0mer/kamino/internal/engine/runners"
	"github.com/t0mer/kamino/internal/events"
	kexec "github.com/t0mer/kamino/internal/exec"
	"github.com/t0mer/kamino/internal/manifest"
	"github.com/t0mer/kamino/internal/plan"
	"github.com/t0mer/kamino/internal/secrets"
	"github.com/t0mer/kamino/internal/state"
)

// This file is an internal (white-box) test, like state's *_internal_test.go
// files: it reaches into unexported fields (writer, runnerLookup) to install
// narrow, test-only seams that let a persistence failure and a panicking
// runner be reproduced deterministically, without depending on a genuine
// database fault or widening StartRequest's public shape.

func newTestManager(t *testing.T) (*Manager, *state.Store, *events.Bus) {
	t.Helper()
	db, err := state.Open(filepath.Join(t.TempDir(), "k.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	bus := events.NewBus(events.DefaultBuffer)
	t.Cleanup(bus.Close)

	return New(db, bus, 50), db, bus
}

func internalTestItem(id string) manifest.Item {
	return manifest.Item{ID: id, Name: id, CategoryID: "c", Type: manifest.ItemApt, Packages: []string{id}}
}

func internalTestPlan(items ...manifest.Item) *plan.Plan {
	p := &plan.Plan{ProfileID: "test", Arch: "amd64", ConfigSHA: "abc123"}
	for _, it := range items {
		p.Steps = append(p.Steps, plan.Step{Ref: it.Ref(), Name: it.Name, Type: string(it.Type), Item: it})
	}
	return p
}

func internalTestRequest(p *plan.Plan) StartRequest {
	return StartRequest{
		Plan:     p,
		Resolved: &manifest.Resolved{SHA: "abc123"},
		Secrets:  secrets.New(),
	}
}

// waitIdle blocks until the manager has no active run, so a background
// goroutine started by a test cannot outlive it. A run that is still
// executing when a test's t.Cleanup closes the *state.Store below it keeps
// writing to a closed database, spamming unrelated later tests with
// "sql: database is closed" warnings and hiding a genuine regression in the
// noise (finding 4).
func waitIdle(t *testing.T, m *Manager) {
	t.Helper()
	require.Eventually(t, func() bool {
		_, busy := m.Active()
		return !busy
	}, 20*time.Second, 5*time.Millisecond, "the run must reach a terminal state before the test returns")
}

// failingStepWriter lets CreateRun succeed for real (so the run row exists,
// exactly as it would just before a genuine CreateStep failure) while
// CreateStep always fails, deterministically reproducing the partial-persist
// scenario from finding 2 without depending on a real database fault.
type failingStepWriter struct {
	db *state.Store
}

func (f failingStepWriter) CreateRun(r state.Run) error { return f.db.CreateRun(r) }

func (f failingStepWriter) CreateStep(state.Step) error {
	return errors.New("injected: simulated step persist failure")
}

// TestStartMarksTheRunFailedWhenStepPersistFails is the regression test for
// finding 2: a CreateStep failure used to leave the run row behind forever
// with status=running and finished_at=NULL, because Start only released the
// in-memory slot and never finished the row it had already inserted.
func TestStartMarksTheRunFailedWhenStepPersistFails(t *testing.T) {
	m, db, _ := newTestManager(t)
	m.writer = failingStepWriter{db: db}

	_, err := m.Start(context.Background(), internalTestRequest(internalTestPlan(internalTestItem("a"))))

	require.Error(t, err, "a step persist failure must surface to the caller")

	runs, err := db.ListRuns(1)
	require.NoError(t, err)
	require.Len(t, runs, 1, "the run row created before the failing step must still exist")
	assert.Equal(t, state.StatusFailed, runs[0].Status,
		"a run that never finished persisting must not be left stuck at status=running forever")
	assert.NotNil(t, runs[0].FinishedAt, "a failed run must have a finished_at, or it reads as still in progress")

	// The slot itself must also be free: a failed Start must not leave the
	// manager permanently believing a run is active.
	_, busy := m.Active()
	assert.False(t, busy, "the run slot must be released after a persist failure")
}

// panicRunner satisfies runners.Runner but panics on every call, standing in
// for a step runner with a genuine programming bug (a nil dereference, an
// out-of-range index, and so on).
type panicRunner struct{}

func (panicRunner) Check(context.Context, runners.ResolvedItem) (bool, error) {
	panic("injected: simulated runner panic")
}

func (panicRunner) Install(context.Context, runners.ResolvedItem) error {
	panic("injected: simulated runner panic")
}

// TestExecuteRecoversFromAPanickingRunner is the regression test for finding
// 3. Before the fix, a panic raised inside execute (here, from a step
// runner) was not recovered anywhere in the call chain and crashed the
// entire process — taking down the HTTP server, every SSE subscriber and all
// in-flight requests in production.
//
// execute is called directly (bypassing the "go m.execute(...)" goroutine
// Start uses) and its bus subscription is set up first, so the test observes
// the panic recovery deterministically: a panicking runner has essentially
// no work to do before panicking, so racing a subscription against the
// asynchronous goroutine Start spawns would make catching the terminal event
// on the wire unreliably flaky. Driving execute synchronously on the test
// goroutine still exercises the real recover() path — recover's mechanics do
// not depend on which goroutine is unwinding.
func TestExecuteRecoversFromAPanickingRunner(t *testing.T) {
	m, db, bus := newTestManager(t)
	m.runnerLookup = func(manifest.ItemType) (runners.Runner, bool) { return panicRunner{}, true }

	const runID = "panic-test-run"
	req := internalTestRequest(internalTestPlan(internalTestItem("a")))

	stepIDs, err := m.persist(runID, req)
	require.NoError(t, err)

	ch, unsub := bus.Subscribe(runID)
	defer unsub()

	// Mirror the bookkeeping Start performs before handing off to execute, so
	// release()'s effect on the slot is observable below.
	ctx, cancel := context.WithCancel(context.Background())
	m.mu.Lock()
	m.active = runID
	m.cancel = cancel
	m.mu.Unlock()
	defer cancel()

	require.NotPanics(t, func() {
		m.execute(ctx, runID, req, stepIDs)
	}, "a panicking runner must not crash the process")

	run, _, err := db.GetRun(runID)
	require.NoError(t, err)
	assert.Equal(t, state.StatusFailed, run.Status, "a run whose runner panicked must be recorded as failed")
	assert.NotNil(t, run.FinishedAt)

	// A terminal run event must still reach subscribers, or a browser
	// watching an SSE stream would hang forever waiting for a run that will
	// never report completion.
	var sawTerminalRunEvent bool
	for !sawTerminalRunEvent {
		select {
		case e := <-ch:
			if e.Type == events.EventRun {
				assert.Equal(t, string(state.StatusFailed), e.Status)
				sawTerminalRunEvent = true
			}
		case <-time.After(5 * time.Second):
			t.Fatal("no terminal run event was published after the panic")
		}
	}

	_, busy := m.Active()
	assert.False(t, busy, "the run slot must be released even after a panic")
}

// TestResolveExecutorsDefaultsToTheRealExecutor pins the safety property in the
// production direction: a StartRequest that injects nothing must run through
// the executors that genuinely install as root, never a fake or a nil. Making
// the executor injectable for tests must not silently leave production
// resolving to something that does nothing.
func TestResolveExecutorsDefaultsToTheRealExecutor(t *testing.T) {
	e, d := resolveExecutors(nil, nil)

	require.NotNil(t, e)
	require.NotNil(t, d)
	assert.IsType(t, &kexec.RealExecutor{}, e,
		"a nil executor in production must resolve to the real one")
	assert.IsType(t, &download.HTTPDownloader{}, d,
		"a nil downloader in production must resolve to the real one")
}

func TestResolveExecutorsKeepsInjectedFakes(t *testing.T) {
	fakeExec := kexec.NewFakeExecutor()
	fakeDL := download.NewFakeDownloader()

	e, d := resolveExecutors(fakeExec, fakeDL)

	assert.Same(t, fakeExec, e, "an injected executor must be used unchanged")
	assert.Same(t, fakeDL, d, "an injected downloader must be used unchanged")
}
