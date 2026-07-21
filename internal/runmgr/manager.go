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

// StartRequest is everything needed to execute one plan.
type StartRequest struct {
	Plan            *plan.Plan
	Resolved        *manifest.Resolved
	Secrets         *secrets.Store
	ContinueOnError bool
	ConfigSource    runners.ScriptSource
}

// Manager admits at most one run at a time and executes it in the background.
type Manager struct {
	db       *state.Store
	bus      *events.Bus
	keepRuns int

	mu     sync.Mutex
	active string
	cancel context.CancelFunc
}

// New builds a manager persisting to db and publishing to bus.
func New(db *state.Store, bus *events.Bus, keepRuns int) *Manager {
	return &Manager{db: db, bus: bus, keepRuns: keepRuns}
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
		return "", fmt.Errorf("%w: %s", ErrRunInProgress, busy)
	}
	m.active = runID
	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	m.cancel = cancel
	m.mu.Unlock()

	if err := m.persist(runID, req); err != nil {
		m.release()
		return "", err
	}

	go m.execute(runCtx, runID, req)
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
// polling immediately after Start sees a real run rather than a 404.
func (m *Manager) persist(runID string, req StartRequest) error {
	if err := m.db.CreateRun(state.Run{
		ID:        runID,
		StartedAt: time.Now().UTC(),
		Profile:   req.Plan.ProfileID,
		ConfigSHA: req.Plan.ConfigSHA,
		Status:    state.StatusRunning,
	}); err != nil {
		return err
	}

	for _, s := range req.Plan.Steps {
		if err := m.db.CreateStep(state.Step{
			ID:      s.Ref,
			RunID:   runID,
			ItemRef: s.Ref,
			Name:    s.Name,
			Status:  state.StatusPending,
		}); err != nil {
			return err
		}
	}
	return nil
}

// execute runs the plan and always releases the run slot.
func (m *Manager) execute(ctx context.Context, runID string, req StartRequest) {
	defer m.release()

	tempDir, err := os.MkdirTemp("", "kamino-run-*")
	if err != nil {
		slog.Error("creating run temp dir", "run_id", runID, "error", err)
		_ = m.db.FinishRun(runID, state.StatusFailed, time.Now().UTC())
		return
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	deps := runners.Deps{
		Exec:     kexec.NewRealExecutor(),
		Download: download.NewHTTPDownloader(nil),
		TempDir:  tempDir,
	}

	stepIDs := map[string]string{}
	for _, s := range req.Plan.Steps {
		stepIDs[s.Ref] = s.Ref
	}

	eng := engine.New(
		deps.Exec,
		engine.NewRegistry(deps, req.ConfigSource, req.Plan.ConfigSHA),
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
