package engine_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/t0mer/kamino/internal/engine"
	"github.com/t0mer/kamino/internal/engine/runners"
	kexec "github.com/t0mer/kamino/internal/exec"
	"github.com/t0mer/kamino/internal/manifest"
	"github.com/t0mer/kamino/internal/plan"
	"github.com/t0mer/kamino/internal/secrets"
	"github.com/t0mer/kamino/internal/state"
)

// recordingSink captures everything the engine emits.
type recordingSink struct {
	statuses []string
	lines    []string
}

func (r *recordingSink) StepStatus(_, stepID string, s state.Status) {
	r.statuses = append(r.statuses, stepID+"="+string(s))
}

func (r *recordingSink) LogLine(_, _, _, line string) {
	r.lines = append(r.lines, line)
}

// stubRunner is a Runner whose behaviour the test controls directly.
type stubRunner struct {
	installed  bool
	installErr error
	installs   *[]string
	ref        string
}

func (s stubRunner) Check(context.Context, engine.ResolvedItem) (bool, error) {
	return s.installed, nil
}

func (s stubRunner) Install(_ context.Context, it engine.ResolvedItem) error {
	if s.installs != nil {
		*s.installs = append(*s.installs, it.Ref)
	}
	return s.installErr
}

func testPlan(items ...manifest.Item) *plan.Plan {
	p := &plan.Plan{ProfileID: "test", Arch: "amd64"}
	for _, it := range items {
		p.Steps = append(p.Steps, plan.Step{Ref: it.Ref(), Name: it.Name, Type: string(it.Type), Item: it})
	}
	return p
}

func aptItem(id string, deps ...string) manifest.Item {
	return manifest.Item{
		ID: id, Name: id, CategoryID: "c", Type: manifest.ItemApt,
		Packages: []string{id}, DependsOn: deps,
	}
}

func lookupAll(r runners.Runner) engine.RunnerFor {
	return func(manifest.ItemType) (runners.Runner, bool) { return r, true }
}

func newEngine(t *testing.T, e kexec.CommandExecutor, r runners.Runner, sink engine.Sink, opts engine.Options) *engine.Engine {
	t.Helper()
	if opts.Secrets == nil {
		opts.Secrets = secrets.New()
	}
	if opts.Arch == "" {
		opts.Arch = "amd64"
	}
	return engine.New(e, lookupAll(r), sink, opts)
}

func TestRunInstallsEveryStep(t *testing.T) {
	var installed []string
	sink := &recordingSink{}
	eng := newEngine(t, kexec.NewFakeExecutor(), stubRunner{installs: &installed}, sink, engine.Options{})

	got, err := eng.Run(context.Background(), testPlan(aptItem("a"), aptItem("b")), "run-1")

	require.NoError(t, err)
	assert.Equal(t, state.StatusSuccess, got.Status)
	assert.Equal(t, []string{"c/a", "c/b"}, installed)
}

func TestRunSkipsAlreadyInstalled(t *testing.T) {
	var installed []string
	eng := newEngine(t, kexec.NewFakeExecutor(),
		stubRunner{installed: true, installs: &installed}, &recordingSink{}, engine.Options{})

	got, err := eng.Run(context.Background(), testPlan(aptItem("a")), "run-1")

	require.NoError(t, err)
	assert.Empty(t, installed, "an already-installed item must not be reinstalled")
	assert.Equal(t, state.StatusSkipped, got.Steps[0].Status)
}

func TestRunAptUpdateRunsOnceAtStart(t *testing.T) {
	fake := kexec.NewFakeExecutor()
	eng := newEngine(t, fake, stubRunner{}, &recordingSink{}, engine.Options{AptUpdate: true})

	_, err := eng.Run(context.Background(), testPlan(aptItem("a"), aptItem("b")), "run-1")

	require.NoError(t, err)
	var updates int
	for _, line := range fake.CommandLines() {
		if line == "/bin/sh -c apt-get update" {
			updates++
		}
	}
	assert.Equal(t, 1, updates, "apt-get update is run-scoped, not per step")
}

func TestRunHaltsOnFailureAndBlocksRest(t *testing.T) {
	eng := newEngine(t, kexec.NewFakeExecutor(),
		stubRunner{installErr: errors.New("boom")}, &recordingSink{}, engine.Options{})

	got, err := eng.Run(context.Background(), testPlan(aptItem("a"), aptItem("b")), "run-1")

	require.NoError(t, err, "a failed step is a Summary, not a Go error")
	assert.Equal(t, state.StatusFailed, got.Status)
	assert.Equal(t, state.StatusFailed, got.Steps[0].Status)
	assert.Equal(t, state.StatusBlocked, got.Steps[1].Status,
		"steps after a halt never ran, so they are blocked rather than failed")
}

func TestContinueOnErrorBlocksOnlyDependents(t *testing.T) {
	eng := newEngine(t, kexec.NewFakeExecutor(),
		stubRunner{installErr: errors.New("boom")}, &recordingSink{},
		engine.Options{ContinueOnError: true})

	// b depends on a; c is unrelated.
	p := testPlan(aptItem("a"), aptItem("b", "c/a"), aptItem("c"))
	got, err := eng.Run(context.Background(), p, "run-1")

	require.NoError(t, err)
	assert.Equal(t, state.StatusFailed, got.Steps[0].Status, "a failed")
	assert.Equal(t, state.StatusBlocked, got.Steps[1].Status, "b depends on a")
	assert.Equal(t, state.StatusFailed, got.Steps[2].Status, "c is unrelated and still attempted")
}

func TestBlockedPropagatesTransitively(t *testing.T) {
	eng := newEngine(t, kexec.NewFakeExecutor(),
		stubRunner{installErr: errors.New("boom")}, &recordingSink{},
		engine.Options{ContinueOnError: true})

	p := testPlan(aptItem("a"), aptItem("b", "c/a"), aptItem("d", "c/b"))
	got, err := eng.Run(context.Background(), p, "run-1")

	require.NoError(t, err)
	assert.Equal(t, state.StatusBlocked, got.Steps[2].Status,
		"d depends on b, which was itself blocked by a's failure")
}

func TestRunEmitsStatusTransitions(t *testing.T) {
	sink := &recordingSink{}
	eng := newEngine(t, kexec.NewFakeExecutor(), stubRunner{}, sink, engine.Options{})

	_, err := eng.Run(context.Background(), testPlan(aptItem("a")), "run-1")

	require.NoError(t, err)
	assert.Contains(t, sink.statuses[0], "=running")
	assert.Contains(t, sink.statuses[len(sink.statuses)-1], "=success")
}

func TestRunRedactsSecretsFromLogs(t *testing.T) {
	store := secrets.New()
	store.Set("TOKEN", "sup3rs3cret")

	fake := kexec.NewFakeExecutor()
	fake.Script("pre-install", kexec.Result{Stdout: []string{"using sup3rs3cret now"}})

	sink := &recordingSink{}
	it := aptItem("a")
	it.PreInstall = []string{"echo pre-install {secret:TOKEN}"}

	eng := newEngine(t, fake, stubRunner{}, sink, engine.Options{Secrets: store})
	_, err := eng.Run(context.Background(), testPlan(it), "run-1")

	require.NoError(t, err)
	for _, line := range sink.lines {
		assert.NotContains(t, line, "sup3rs3cret", "secrets must never reach a sink")
	}
	assert.Contains(t, sink.lines, "using *** now")
}

func TestRunCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	eng := newEngine(t, kexec.NewFakeExecutor(), stubRunner{}, &recordingSink{}, engine.Options{})
	got, err := eng.Run(ctx, testPlan(aptItem("a")), "run-1")

	require.NoError(t, err)
	assert.Equal(t, state.StatusCancelled, got.Status)
}

func TestRunUnknownItemTypeFailsTheStep(t *testing.T) {
	eng := engine.New(
		kexec.NewFakeExecutor(),
		func(manifest.ItemType) (runners.Runner, bool) { return nil, false },
		&recordingSink{},
		engine.Options{Arch: "amd64", Secrets: secrets.New()},
	)

	got, err := eng.Run(context.Background(), testPlan(aptItem("a")), "run-1")

	require.NoError(t, err)
	assert.Equal(t, state.StatusFailed, got.Steps[0].Status)
	require.Error(t, got.Steps[0].Err)
}

func TestRunPreAndPostInstallCommands(t *testing.T) {
	fake := kexec.NewFakeExecutor()
	it := aptItem("a")
	it.PreInstall = []string{"echo before"}
	it.PostInstall = []string{"echo after"}

	eng := newEngine(t, fake, stubRunner{}, &recordingSink{}, engine.Options{})
	_, err := eng.Run(context.Background(), testPlan(it), "run-1")

	require.NoError(t, err)
	assert.Contains(t, fake.CommandLines(), "/bin/sh -c echo before")
	assert.Contains(t, fake.CommandLines(), "/bin/sh -c echo after")
}
