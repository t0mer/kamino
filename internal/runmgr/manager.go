// Package runmgr owns the lifecycle of an install run started over HTTP.
//
// The CLI's apply runs an install synchronously and exits. An HTTP caller
// cannot wait that long, so this package starts the engine on a goroutine and
// hands back a run id immediately; progress reaches the caller over the event
// bus instead of a return value.
package runmgr

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"runtime/debug"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/t0mer/kamino/internal/download"
	"github.com/t0mer/kamino/internal/engine"
	"github.com/t0mer/kamino/internal/engine/runners"
	"github.com/t0mer/kamino/internal/events"
	kexec "github.com/t0mer/kamino/internal/exec"
	"github.com/t0mer/kamino/internal/manifest"
	"github.com/t0mer/kamino/internal/plan"
	"github.com/t0mer/kamino/internal/secrets"
	"github.com/t0mer/kamino/internal/state"
)

// ErrRunInProgress reports that a run is already executing. Only one run may
// execute at a time: dpkg takes a machine-wide lock, so a second concurrent
// install would block or corrupt the first rather than working.
var ErrRunInProgress = errors.New("a run is already in progress")

// RunInProgressError reports that a run is already executing and names the
// active run's id. Start returns this (rather than the bare ErrRunInProgress)
// so a caller can report which run is busy without a second, racy call to
// Active() — the active run may have already finished by the time such a
// follow-up call happened, leaving nothing to name.
//
// RunInProgressError unwraps to ErrRunInProgress, so existing callers using
// errors.Is(err, ErrRunInProgress) keep working unchanged.
type RunInProgressError struct {
	// ActiveID is the id of the run that is currently executing.
	ActiveID string
}

// Error renders the same text Start has always produced for this condition.
func (e *RunInProgressError) Error() string {
	return fmt.Sprintf("%s: %s", ErrRunInProgress, e.ActiveID)
}

// Unwrap exposes ErrRunInProgress so errors.Is(err, ErrRunInProgress) matches.
func (e *RunInProgressError) Unwrap() error { return ErrRunInProgress }

// StartRequest is everything needed to execute one plan.
type StartRequest struct {
	Plan            *plan.Plan
	Resolved        *manifest.Resolved
	Secrets         *secrets.Store
	ContinueOnError bool
	ConfigSource    runners.ScriptSource

	// Exec, when non-nil, replaces kexec.NewRealExecutor() as the command
	// executor the run's steps execute through. Production leaves this nil
	// so every run uses the real executor exactly as before; tests inject a
	// kexec.FakeExecutor so a run resolves entirely in memory — the config
	// repo is untrusted input and its steps run as root, so nothing outside
	// this package's own tests should ever cause a real command to execute.
	Exec kexec.CommandExecutor
	// Download, when non-nil, replaces download.NewHTTPDownloader(nil) as
	// the artifact fetcher the run's steps use. Production leaves this nil;
	// tests inject a download.FakeDownloader for the same reason as Exec.
	Download download.Downloader
}

// runStepWriter is the subset of *state.Store that persist needs to create a
// run and its steps. Production always passes the real *state.Store (which
// satisfies this interface); tests in this package may substitute a fake
// that fails CreateStep deterministically, to exercise the
// run-gets-marked-failed cleanup path without depending on a genuine
// database fault.
type runStepWriter interface {
	CreateRun(state.Run) error
	CreateStep(state.Step) error
}

// Manager admits at most one run at a time and executes it in the background.
type Manager struct {
	db       *state.Store
	writer   runStepWriter
	bus      *events.Bus
	keepRuns int

	// runnerLookup, when set, replaces engine.NewRegistry for the run. It
	// exists solely so tests in this package can inject a runner that
	// misbehaves (e.g. panics) without widening the public StartRequest API.
	// Always nil in production.
	runnerLookup engine.RunnerFor

	mu     sync.Mutex
	active string
	cancel context.CancelFunc
}

// New builds a manager persisting to db and publishing to bus.
func New(db *state.Store, bus *events.Bus, keepRuns int) *Manager {
	return &Manager{db: db, writer: db, bus: bus, keepRuns: keepRuns}
}

// Active reports the in-flight run's id, if any.
func (m *Manager) Active() (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.active, m.active != ""
}

// Start persists a run and executes it in the background, returning as soon as
// the run exists. It returns ErrRunInProgress if a run is already executing.
func (m *Manager) Start(ctx context.Context, req StartRequest) (string, error) {
	runID := uuid.NewString()

	m.mu.Lock()
	if m.active != "" {
		busy := m.active
		m.mu.Unlock()
		return "", &RunInProgressError{ActiveID: busy}
	}
	m.active = runID
	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	m.cancel = cancel
	m.mu.Unlock()

	stepIDs, err := m.persist(runID, req)
	if err != nil {
		m.release()
		return "", err
	}

	go m.execute(runCtx, runID, req, stepIDs)
	return runID, nil
}

// Cancel stops the named run. The engine's context cancellation tears down the
// running step's process group.
func (m *Manager) Cancel(runID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active != runID {
		return fmt.Errorf("run %q is not running", runID)
	}
	if m.cancel != nil {
		m.cancel()
	}
	return nil
}

// persist writes the run and its steps before execution begins, so a client
// polling immediately after Start sees a real run rather than a 404. It
// returns a map from each step's plan item ref (e.g. "tools/docker") to the
// uuid primary key CreateStep gave it in sqlite, for execute to hand to
// state.NewSink.
//
// Step ids are generated here with uuid.NewString — the same approach
// cmd/kamino/apply.go's execute uses — rather than reusing the item ref as
// the primary key. steps.id is a global PRIMARY KEY, not scoped by run_id
// (see schema.go), so two runs of the same profile would otherwise try to
// insert the same step id twice and the second run's CreateStep would fail
// with a UNIQUE constraint violation.
//
// The engine and the events it publishes over the bus (see execute) always
// identify a step by its item ref, never by this database id: the ref is
// meaningful outside the process (it is the plan's own vocabulary), while
// the uuid is purely a sqlite implementation detail. Any future code that
// serves persisted run/step history to the browser must expose ItemRef, not
// this ID, as the step identifier — otherwise a client comparing a live SSE
// StepID against a replayed history entry would see two different values
// for the same step.
func (m *Manager) persist(runID string, req StartRequest) (map[string]string, error) {
	if err := m.writer.CreateRun(state.Run{
		ID:        runID,
		StartedAt: time.Now().UTC(),
		Profile:   req.Plan.ProfileID,
		ConfigSHA: req.Plan.ConfigSHA,
		Status:    state.StatusRunning,
	}); err != nil {
		return nil, err
	}

	stepIDs := make(map[string]string, len(req.Plan.Steps))
	for _, s := range req.Plan.Steps {
		id := uuid.NewString()
		if err := m.writer.CreateStep(state.Step{
			ID:      id,
			RunID:   runID,
			ItemRef: s.Ref,
			Name:    s.Name,
			Status:  state.StatusPending,
		}); err != nil {
			// The run row already exists at this point. Leaving it behind
			// with status=running and finished_at=NULL would strand it as a
			// phantom "in progress" run forever: nothing else ever
			// transitions a run that never started executing. Close it out
			// as failed before surfacing the error, so history views don't
			// show a run that will never finish.
			if finishErr := m.db.FinishRun(runID, state.StatusFailed, time.Now().UTC()); finishErr != nil {
				slog.Warn("marking run failed after step persist error",
					"run_id", runID, "error", finishErr)
			}
			return nil, fmt.Errorf("persisting step %q: %w", s.Ref, err)
		}
		stepIDs[s.Ref] = id
	}
	return stepIDs, nil
}

// execute runs the plan and always releases the run slot. stepIDs maps each
// plan step's item ref to the database step id persist already created for
// it.
func (m *Manager) execute(ctx context.Context, runID string, req StartRequest, stepIDs map[string]string) {
	defer m.release()
	// Registered after release so it runs first during a panic unwind: see
	// recoverFromPanic's doc comment for why a step runner panicking must
	// not be allowed to take the whole process down with it.
	defer m.recoverFromPanic(runID)

	tempDir, err := os.MkdirTemp("", "kamino-run-*")
	if err != nil {
		slog.Error("creating run temp dir", "run_id", runID, "error", err)
		_ = m.db.FinishRun(runID, state.StatusFailed, time.Now().UTC())
		return
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	exec, dl := resolveExecutors(req.Exec, req.Download)
	deps := runners.Deps{
		Exec:     exec,
		Download: dl,
		TempDir:  tempDir,
	}

	lookup := m.runnerLookup
	if lookup == nil {
		lookup = engine.NewRegistry(deps, req.ConfigSource, req.Plan.ConfigSHA, req.Secrets)
	}

	eng := engine.New(
		deps.Exec,
		lookup,
		engine.NewMultiSink(
			state.NewSink(m.db, stepIDs),
			events.NewSink(m.bus),
		),
		engine.Options{
			ContinueOnError: req.ContinueOnError,
			AptUpdate:       req.Resolved.Manifest.Defaults.AptUpdateBeforeRun,
			Arch:            req.Plan.Arch,
			Defaults:        req.Resolved.Manifest.Defaults,
			Secrets:         req.Secrets,
		},
	)

	summary, runErr := eng.Run(ctx, req.Plan, runID)

	status := summary.Status
	if runErr != nil {
		slog.Error("run failed to execute", "run_id", runID, "error", runErr)
		status = state.StatusFailed
	}
	if err := m.db.FinishRun(runID, status, time.Now().UTC()); err != nil {
		slog.Warn("recording run completion failed", "run_id", runID, "error", err)
	}
	if err := m.db.Prune(m.keepRuns); err != nil {
		slog.Warn("pruning old run history failed", "error", err)
	}

	m.bus.Publish(events.Event{Type: events.EventRun, RunID: runID, Status: string(status)})
}

// recoverFromPanic stops a panic inside execute (most likely from a step
// runner) from propagating out of the goroutine and crashing the whole
// process — which, unlike a leaked run slot, would take the HTTP server and
// every SSE subscriber down with it. It must be deferred directly in
// execute, since recover only has an effect when called by a function
// invoked directly via defer.
//
// On recovery: the panic value and a stack trace are logged at error level
// (never a captured secret — panic values here are Go programming errors,
// not step output, which is redacted before it ever reaches a sink), the run
// is marked failed, and a terminal run event is published so a client
// watching over SSE is not left waiting forever for a run that will never
// report completion. The deferred call to release still runs afterwards, so
// the slot is freed exactly as it is on any other exit from execute.
func (m *Manager) recoverFromPanic(runID string) {
	r := recover()
	if r == nil {
		return
	}
	slog.Error("run panicked", "run_id", runID, "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
	if err := m.db.FinishRun(runID, state.StatusFailed, time.Now().UTC()); err != nil {
		slog.Warn("recording run completion after panic failed", "run_id", runID, "error", err)
	}
	m.bus.Publish(events.Event{Type: events.EventRun, RunID: runID, Status: string(state.StatusFailed)})
}

// release frees the run slot. It runs from a defer, so it must not panic even
// if the run died unexpectedly.
func (m *Manager) release() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	m.active = ""
}

// resolveExecutors picks the executor and downloader a run will use. A nil
// request field means "use the real thing" — a test injects fakes so a run
// resolves in memory, but production, which sets neither, must always fall
// through to executors that genuinely install as root.
func resolveExecutors(e kexec.CommandExecutor, d download.Downloader) (kexec.CommandExecutor, download.Downloader) {
	if e == nil {
		e = kexec.NewRealExecutor()
	}
	if d == nil {
		d = download.NewHTTPDownloader(nil)
	}
	return e, d
}
