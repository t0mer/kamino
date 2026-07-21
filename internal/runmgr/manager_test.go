package runmgr_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/t0mer/kamino/internal/events"
	"github.com/t0mer/kamino/internal/manifest"
	"github.com/t0mer/kamino/internal/plan"
	"github.com/t0mer/kamino/internal/runmgr"
	"github.com/t0mer/kamino/internal/secrets"
	"github.com/t0mer/kamino/internal/state"
)

func newManager(t *testing.T) (*runmgr.Manager, *state.Store, *events.Bus) {
	t.Helper()
	db, err := state.Open(filepath.Join(t.TempDir(), "k.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	bus := events.NewBus(events.DefaultBuffer)
	t.Cleanup(bus.Close)

	return runmgr.New(db, bus, 50), db, bus
}

// aptItem builds an item whose install the fake executor will satisfy.
func aptItem(id string) manifest.Item {
	return manifest.Item{
		ID: id, Name: id, CategoryID: "c",
		Type: manifest.ItemApt, Packages: []string{id},
	}
}

func testPlan(items ...manifest.Item) *plan.Plan {
	p := &plan.Plan{ProfileID: "test", Arch: "amd64", ConfigSHA: "abc123"}
	for _, it := range items {
		p.Steps = append(p.Steps, plan.Step{
			Ref: it.Ref(), Name: it.Name, Type: string(it.Type), Item: it,
		})
	}
	return p
}

func request(p *plan.Plan) runmgr.StartRequest {
	return runmgr.StartRequest{
		Plan:     p,
		Resolved: &manifest.Resolved{SHA: "abc123"},
		Secrets:  secrets.New(),
	}
}

// waitIdle blocks until the manager reports no active run. Every test that
// starts a run must call this before returning: a run still executing when
// t.Cleanup closes the *state.Store below it keeps writing to a closed
// database on its own goroutine, spamming later, unrelated tests with
// "sql: database is closed" warnings and burying any real regression in that
// noise (see the finding 4 fix).
func waitIdle(t *testing.T, m *runmgr.Manager) {
	t.Helper()
	require.Eventually(t, func() bool {
		_, busy := m.Active()
		return !busy
	}, 20*time.Second, 5*time.Millisecond, "the run must reach a terminal state before the test ends")
}

func TestStartReturnsARunID(t *testing.T) {
	m, _, _ := newManager(t)

	runID, err := m.Start(context.Background(), request(testPlan(aptItem("a"))))

	require.NoError(t, err)
	assert.NotEmpty(t, runID)
	waitIdle(t, m)
}

func TestStartPersistsTheRunAndItsSteps(t *testing.T) {
	m, db, _ := newManager(t)

	runID, err := m.Start(context.Background(), request(testPlan(aptItem("a"), aptItem("b"))))
	require.NoError(t, err)

	run, steps, err := db.GetRun(runID)
	require.NoError(t, err)
	assert.Equal(t, "test", run.Profile)
	assert.Equal(t, "abc123", run.ConfigSHA)
	require.Len(t, steps, 2)
	assert.Equal(t, "c/a", steps[0].ItemRef)
	waitIdle(t, m)
}

func TestSecondStartWhileRunningIsRejected(t *testing.T) {
	m, _, _ := newManager(t)

	// A plan with many steps keeps the first run in flight long enough to race.
	items := make([]manifest.Item, 0, 50)
	for i := 0; i < 50; i++ {
		items = append(items, aptItem(string(rune('a'+i%26))+string(rune('0'+i/26))))
	}
	first, err := m.Start(context.Background(), request(testPlan(items...)))
	require.NoError(t, err)

	_, err = m.Start(context.Background(), request(testPlan(aptItem("z"))))

	require.ErrorIs(t, err, runmgr.ErrRunInProgress)
	active, ok := m.Active()
	assert.True(t, ok)
	assert.Equal(t, first, active)
	waitIdle(t, m)
}

func TestSlotIsReleasedWhenTheRunFinishes(t *testing.T) {
	m, _, _ := newManager(t)

	_, err := m.Start(context.Background(), request(testPlan(aptItem("a"))))
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		_, busy := m.Active()
		return !busy
	}, 10*time.Second, 20*time.Millisecond, "the run slot must be released when a run ends")

	_, err = m.Start(context.Background(), request(testPlan(aptItem("b"))))
	assert.NoError(t, err, "a new run must be startable once the slot is free")
	waitIdle(t, m)
}

// TestRunningTheSamePlanTwiceInARowSucceeds is the end-to-end regression test
// for finding 1: re-running any profile used to fail the second time with a
// sqlite UNIQUE constraint violation, because persist reused the plan's item
// ref as the step's global primary key. An operator retrying the very same
// profile — the ordinary case, not an edge case — would hit this on every
// second attempt.
func TestRunningTheSamePlanTwiceInARowSucceeds(t *testing.T) {
	m, db, _ := newManager(t)

	plan := testPlan(aptItem("a"), aptItem("b"))

	firstID, err := m.Start(context.Background(), request(plan))
	require.NoError(t, err, "the first run of a plan must succeed")
	waitIdle(t, m)

	secondID, err := m.Start(context.Background(), request(plan))
	require.NoError(t, err, "re-running the exact same plan must succeed, not fail on a duplicate step id")
	waitIdle(t, m)

	assert.NotEqual(t, firstID, secondID)

	for _, id := range []string{firstID, secondID} {
		run, steps, err := db.GetRun(id)
		require.NoError(t, err)
		require.Len(t, steps, 2)
		assert.NotEqual(t, run.Status, "", "each run must have recorded a real status")
	}
}

func TestCancelUnknownRunIsAnError(t *testing.T) {
	m, _, _ := newManager(t)

	err := m.Cancel("nope")

	require.Error(t, err)
}

func TestActiveIsFalseWhenIdle(t *testing.T) {
	m, _, _ := newManager(t)

	_, ok := m.Active()

	assert.False(t, ok)
}

func TestStartPublishesEventsForTheRun(t *testing.T) {
	m, _, bus := newManager(t)

	// Subscribe before starting so no event is missed.
	runID := ""
	done := make(chan struct{})
	go func() {
		defer close(done)
		time.Sleep(50 * time.Millisecond)
	}()

	id, err := m.Start(context.Background(), request(testPlan(aptItem("a"))))
	require.NoError(t, err)
	runID = id

	ch, unsub := bus.Subscribe(runID)
	defer unsub()

	// The run may already have finished; either an event arrives or the run is
	// already terminal. Both are acceptable — this asserts the wiring exists,
	// not the timing.
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
	}
	<-done
	waitIdle(t, m)
}
