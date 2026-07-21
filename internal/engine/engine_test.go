package engine_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

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

func (r *recordingSink) StepStatus(_, stepID string, s state.Status, _ int) {
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

// TestRunPreInstallErrorNeverLeaksSecret pins Finding 1: a failing
// pre_install line is templated with the real secret value before it runs,
// so the error built around it must redact that value. Before the fix,
// StepResult.Err.Error() embedded the plaintext secret via the raw command
// line in its %q wrap, even though runShell's own error text was already
// redacted.
func TestRunPreInstallErrorNeverLeaksSecret(t *testing.T) {
	store := secrets.New()
	store.Set("TOKEN", "sup3rs3cret")

	fake := kexec.NewFakeExecutor()
	fake.Script("cloudflared service install", kexec.Result{ExitCode: 1, Stdout: []string{"boom"}})

	it := aptItem("a")
	it.PreInstall = []string{"cloudflared service install {secret:TOKEN}"}

	eng := newEngine(t, fake, stubRunner{}, &recordingSink{}, engine.Options{Secrets: store})
	got, err := eng.Run(context.Background(), testPlan(it), "run-1")

	require.NoError(t, err)
	require.Error(t, got.Steps[0].Err)
	assert.NotContains(t, got.Steps[0].Err.Error(), "sup3rs3cret",
		"a failing pre_install error must never contain the plaintext secret")
	assert.Equal(t, state.StatusFailed, got.Steps[0].Status)
}

// TestRunPostInstallErrorNeverLeaksSecret is TestRunPreInstallErrorNeverLeaksSecret's
// counterpart for post_install, the other error path the reviewer named.
func TestRunPostInstallErrorNeverLeaksSecret(t *testing.T) {
	store := secrets.New()
	store.Set("TOKEN", "sup3rs3cret")

	fake := kexec.NewFakeExecutor()
	fake.Script("cloudflared service install", kexec.Result{ExitCode: 1, Stdout: []string{"boom"}})

	it := aptItem("a")
	it.PostInstall = []string{"cloudflared service install {secret:TOKEN}"}

	eng := newEngine(t, fake, stubRunner{}, &recordingSink{}, engine.Options{Secrets: store})
	got, err := eng.Run(context.Background(), testPlan(it), "run-1")

	require.NoError(t, err)
	require.Error(t, got.Steps[0].Err)
	assert.NotContains(t, got.Steps[0].Err.Error(), "sup3rs3cret",
		"a failing post_install error must never contain the plaintext secret")
	assert.Equal(t, state.StatusFailed, got.Steps[0].Status)
}

// TestRunPreInstallExitCodeReachesStepResult pins Finding 3: a non-zero exit
// from a pre_install command is known (runShell captured it from
// exec.Result) and must be carried through to StepResult.ExitCode rather
// than left at its zero value.
func TestRunPreInstallExitCodeReachesStepResult(t *testing.T) {
	fake := kexec.NewFakeExecutor()
	fake.Script("false-ish", kexec.Result{ExitCode: 17})

	it := aptItem("a")
	it.PreInstall = []string{"false-ish"}

	eng := newEngine(t, fake, stubRunner{}, &recordingSink{}, engine.Options{})
	got, err := eng.Run(context.Background(), testPlan(it), "run-1")

	require.NoError(t, err)
	require.Error(t, got.Steps[0].Err)
	assert.Equal(t, 17, got.Steps[0].ExitCode)
}

// TestRunFailedStepExitCodeReachesSQLite is the end-to-end regression for
// Finding 3: TestRunPreInstallExitCodeReachesStepResult (above) already
// proved the engine computes the right StepResult.ExitCode, but that value
// used to die at the Sink boundary — engine.Sink.StepStatus had no way to
// carry it, so internal/state.Sink hardcoded UpdateStepStatus's exit code to
// 0 no matter what the command actually returned. This test drives a failing
// step through the real engine into a real sqlite-backed state.Sink and
// reads the row back, so a regression at either end of that seam fails here.
func TestRunFailedStepExitCodeReachesSQLite(t *testing.T) {
	db, err := state.Open(filepath.Join(t.TempDir(), "k.db"))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	require.NoError(t, db.CreateRun(state.Run{
		ID: "run-1", Status: state.StatusRunning, StartedAt: time.Now().UTC(),
	}))
	require.NoError(t, db.CreateStep(state.Step{
		ID: "step-1", RunID: "run-1", ItemRef: "c/a", Status: state.StatusPending,
	}))

	fake := kexec.NewFakeExecutor()
	fake.Script("false-ish", kexec.Result{ExitCode: 42})

	it := aptItem("a")
	it.PreInstall = []string{"false-ish"}

	sink := state.NewSink(db, map[string]string{"c/a": "step-1"})
	eng := newEngine(t, fake, stubRunner{}, sink, engine.Options{})

	_, err = eng.Run(context.Background(), testPlan(it), "run-1")
	require.NoError(t, err)

	_, steps, err := db.GetRun("run-1")
	require.NoError(t, err)
	require.Len(t, steps, 1)
	assert.Equal(t, state.StatusFailed, steps[0].Status)
	assert.Equal(t, 42, steps[0].ExitCode,
		"the command's real exit code must reach sqlite, not the hardcoded 0")
}

// TestRunStepTimeoutHaltsAndBlocksWhenNotContinuing pins Finding 2: a step
// whose Install call ends with context.DeadlineExceeded is reported
// StatusCancelled, but that must not be treated as harmless. With
// continue-on-error false, it must halt the run (like a StatusFailed step
// does) and its dependent must never run.
func TestRunStepTimeoutHaltsAndBlocksWhenNotContinuing(t *testing.T) {
	var installs []string
	eng := newEngine(t, kexec.NewFakeExecutor(),
		stubRunner{installErr: context.DeadlineExceeded, installs: &installs},
		&recordingSink{}, engine.Options{})

	p := testPlan(aptItem("a"), aptItem("b", "c/a"))
	got, err := eng.Run(context.Background(), p, "run-1")

	require.NoError(t, err)
	assert.Equal(t, state.StatusCancelled, got.Steps[0].Status, "a's own install timed out")
	assert.Equal(t, state.StatusBlocked, got.Steps[1].Status,
		"b depends on a, whose install never completed, so it must not run")
	assert.Equal(t, []string{"c/a"}, installs, "b's Install must never be called")
	assert.NotEqual(t, state.StatusSuccess, got.Status,
		"a run containing a cancelled step must never report overall success")
}

// TestRunStepTimeoutBlocksOnlyDependentsWhenContinuing is
// TestRunStepTimeoutHaltsAndBlocksWhenNotContinuing's continue-on-error
// counterpart: an unrelated step must still run, but the dependent of the
// timed-out step must be blocked, and the run must still not report success.
func TestRunStepTimeoutBlocksOnlyDependentsWhenContinuing(t *testing.T) {
	eng := newEngine(t, kexec.NewFakeExecutor(),
		stubRunner{installErr: context.DeadlineExceeded},
		&recordingSink{}, engine.Options{ContinueOnError: true})

	// b depends on a; c is unrelated.
	p := testPlan(aptItem("a"), aptItem("b", "c/a"), aptItem("c"))
	got, err := eng.Run(context.Background(), p, "run-1")

	require.NoError(t, err)
	assert.Equal(t, state.StatusCancelled, got.Steps[0].Status, "a timed out")
	assert.Equal(t, state.StatusBlocked, got.Steps[1].Status, "b depends on a")
	assert.Equal(t, state.StatusCancelled, got.Steps[2].Status,
		"c is unrelated and still attempted (and also times out, per the stub)")
	assert.NotEqual(t, state.StatusSuccess, got.Status,
		"a run containing a cancelled step must never report overall success")
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

// leakyRunner mimics a real runner that names the resource it was working on
// in its error. Resolve expands {secret:NAME} into Source, so that message can
// carry a live credential.
type leakyRunner struct {
	err error
}

func (l leakyRunner) Check(context.Context, engine.ResolvedItem) (bool, error) { return false, nil }

func (l leakyRunner) Install(_ context.Context, it engine.ResolvedItem) error {
	if l.err != nil {
		return l.err
	}
	return fmt.Errorf("downloading %s: connection reset", it.Source)
}

func TestRunRunnerErrorNeverLeaksSecret(t *testing.T) {
	store := secrets.New()
	store.Set("DL_TOKEN", "sup3rs3cret")

	it := aptItem("a")
	it.Source = manifest.Source{"amd64": "https://example.com/artifact?token={secret:DL_TOKEN}"}

	eng := newEngine(t, kexec.NewFakeExecutor(), leakyRunner{}, &recordingSink{},
		engine.Options{Secrets: store})
	got, err := eng.Run(context.Background(), testPlan(it), "run-1")

	require.NoError(t, err)
	require.Error(t, got.Steps[0].Err)
	assert.NotContains(t, got.Steps[0].Err.Error(), "sup3rs3cret",
		"a runner error must not carry a credential into the persisted run result")
	assert.Contains(t, got.Steps[0].Err.Error(), "***")
}

func TestRunRedactedRunnerErrorStillUnwraps(t *testing.T) {
	store := secrets.New()
	store.Set("DL_TOKEN", "sup3rs3cret")

	it := aptItem("a")
	it.Source = manifest.Source{"amd64": "https://example.com/x?token={secret:DL_TOKEN}"}

	// An error that both mentions the secret and wraps a sentinel: redaction
	// must not break the sentinel match that drives timeout handling.
	wrapped := fmt.Errorf("fetching https://example.com/x?token=sup3rs3cret: %w", context.DeadlineExceeded)

	eng := newEngine(t, kexec.NewFakeExecutor(), leakyRunner{err: wrapped}, &recordingSink{},
		engine.Options{Secrets: store})
	got, err := eng.Run(context.Background(), testPlan(it), "run-1")

	require.NoError(t, err)
	assert.NotContains(t, got.Steps[0].Err.Error(), "sup3rs3cret")
	assert.True(t, errors.Is(got.Steps[0].Err, context.DeadlineExceeded),
		"redaction must preserve the error chain, or timeout detection breaks")
}
