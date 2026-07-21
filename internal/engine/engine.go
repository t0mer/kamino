package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/t0mer/kamino/internal/engine/runners"
	kexec "github.com/t0mer/kamino/internal/exec"
	"github.com/t0mer/kamino/internal/manifest"
	"github.com/t0mer/kamino/internal/plan"
	"github.com/t0mer/kamino/internal/secrets"
	"github.com/t0mer/kamino/internal/state"
)

// RunnerFor resolves an item type to the runner that installs it.
type RunnerFor func(t manifest.ItemType) (runners.Runner, bool)

// Options configures a run.
type Options struct {
	ContinueOnError bool
	AptUpdate       bool
	Arch            string
	Defaults        manifest.Defaults
	Secrets         *secrets.Store
}

// StepResult is the outcome of one step.
type StepResult struct {
	Ref      string
	Status   state.Status
	ExitCode int
	Err      error
}

// Summary is the outcome of a whole run.
type Summary struct {
	RunID  string
	Status state.Status
	Steps  []StepResult
}

// Engine executes plans.
type Engine struct {
	exec   kexec.CommandExecutor
	lookup RunnerFor
	sink   Sink
	opts   Options
}

// New builds an engine. A nil sink discards progress.
func New(e kexec.CommandExecutor, lookup RunnerFor, sink Sink, opts Options) *Engine {
	if sink == nil {
		sink = nopSink{}
	}
	if opts.Secrets == nil {
		opts.Secrets = secrets.New()
	}
	return &Engine{exec: e, lookup: lookup, sink: sink, opts: opts}
}

// Run executes every step in p, in order.
//
// A step that fails is reported in the Summary, not returned as an error: only
// a failure to run the plan at all (bad secrets, unusable config) is an error.
//
// Precondition: p.Steps must already be topologically sorted — every step's
// dependencies must appear before it. plan.Build guarantees this ordering;
// Run trusts it and does not re-check it. isBlocked only ever consults the
// blocked map for refs seen earlier in the slice, so a step run before its
// (failed or blocked) dependency will install as if that dependency had
// succeeded. Run does not re-verify the ordering itself because plan.Build is
// its only caller today and already does that work once, at plan time; a
// second, per-run traversal here would just repeat it on every apply for a
// condition the caller already enforces.
func (e *Engine) Run(ctx context.Context, p *plan.Plan, runID string) (Summary, error) {
	// The redactor is a point-in-time snapshot of e.opts.Secrets (see
	// secrets.NewRedactor's doc comment): any secret registered on the store
	// after this call will NOT be redacted from the log lines this run
	// writes to sinks and, via the state sink, to sqlite on disk. This is
	// safe here only because the engine itself never calls Store.Set — every
	// secret for the run must already be registered by the caller (server/
	// CLI layer) before Run is invoked. If the engine ever gains a code path
	// that adds secrets mid-run (e.g. prompting for one lazily), the
	// redactor MUST be rebuilt after that happens, or later lines will leak
	// the new secret's plaintext value into the database. Do not move this
	// call later without re-checking that invariant.
	redactor := secrets.NewRedactor(e.opts.Secrets)
	summary := Summary{RunID: runID, Status: state.StatusSuccess}

	blocked := map[string]bool{}
	halted := false

	if e.opts.AptUpdate {
		if _, err := e.runShell(ctx, runID, "apt-update", "apt-get update", redactor, 0); err != nil {
			return Summary{}, fmt.Errorf("running apt-get update: %w", err)
		}
	}

	for _, step := range p.Steps {
		stepID := step.Ref

		if ctx.Err() != nil {
			e.sink.StepStatus(runID, stepID, state.StatusCancelled, 0)
			summary.Steps = append(summary.Steps, StepResult{Ref: step.Ref, Status: state.StatusCancelled})
			summary.Status = state.StatusCancelled
			continue
		}

		if halted || e.isBlocked(step, blocked) {
			blocked[step.Ref] = true
			e.sink.StepStatus(runID, stepID, state.StatusBlocked, 0)
			summary.Steps = append(summary.Steps, StepResult{Ref: step.Ref, Status: state.StatusBlocked})
			continue
		}

		result := e.runStep(ctx, runID, stepID, step, redactor)
		summary.Steps = append(summary.Steps, result)
		e.sink.StepStatus(runID, stepID, result.Status, result.ExitCode)

		// A step that timed out or was cancelled mid-install is just as
		// unusable to its dependents as one that failed outright — its
		// install never completed, so anything depending on it cannot be
		// trusted to proceed. Give it the same halt/blocked treatment as
		// StatusFailed. The run-level status is reported as StatusFailed
		// (not StatusCancelled) here: StatusCancelled at the run level is
		// reserved for the run's own context being cancelled/timed out
		// (handled separately, above and below), which means an operator or
		// caller stopped the run itself. A single step's install deadline
		// expiring is a failure of that step, not a cancellation of the run —
		// the run kept going per its continue-on-error policy. If the run's
		// own context is ALSO cancelled, the check after the loop below
		// upgrades this back to StatusCancelled.
		if result.Status == state.StatusFailed || result.Status == state.StatusCancelled {
			summary.Status = state.StatusFailed
			blocked[step.Ref] = true
			if !e.opts.ContinueOnError {
				halted = true
			}
		}
	}

	if ctx.Err() != nil {
		summary.Status = state.StatusCancelled
	}
	return summary, nil
}

// isBlocked reports whether any dependency of step failed or was blocked.
// Running a step whose dependency demonstrably failed produces confusing
// garbage, so it is marked blocked instead of attempted.
func (e *Engine) isBlocked(step plan.Step, blocked map[string]bool) bool {
	for _, dep := range step.Item.DependsOn {
		if blocked[dep] {
			return true
		}
	}
	return false
}

// redactedError masks secret values in an error's message while leaving the
// error chain intact, so errors.Is still matches sentinels like
// context.DeadlineExceeded.
type redactedError struct {
	msg string
	err error
}

func (e redactedError) Error() string { return e.msg }
func (e redactedError) Unwrap() error { return e.err }

// redactErr masks any secret value appearing in err's message.
//
// Resolve expands {secret:NAME} into an item's Source, Check, CheckContains and
// Packages, so a runner that mentions any of those in its error — a downloader
// naming the URL it failed to fetch, say — would otherwise carry a live
// credential into StepResult.Err, which callers persist to disk.
func redactErr(r *secrets.Redactor, err error) error {
	if err == nil {
		return nil
	}
	masked := r.Redact(err.Error())
	if masked == err.Error() {
		return err
	}
	return redactedError{msg: masked, err: err}
}

func (e *Engine) runStep(ctx context.Context, runID, stepID string, step plan.Step, r *secrets.Redactor) StepResult {
	e.sink.StepStatus(runID, stepID, state.StatusRunning, 0)

	item, err := Resolve(step.Item, e.opts.Arch, e.opts.Defaults, e.opts.Secrets)
	if err != nil {
		return StepResult{Ref: step.Ref, Status: state.StatusFailed, Err: err}
	}

	runner, ok := e.lookup(item.Type)
	if !ok {
		return StepResult{
			Ref: step.Ref, Status: state.StatusFailed,
			Err: fmt.Errorf("no runner for item type %q", item.Type),
		}
	}

	installed, err := runner.Check(ctx, item)
	if err != nil {
		return StepResult{Ref: step.Ref, Status: state.StatusFailed, Err: redactErr(r, err)}
	}
	if installed {
		return StepResult{Ref: step.Ref, Status: state.StatusSkipped}
	}

	for _, line := range item.PreInstall {
		res, err := e.runShell(ctx, runID, stepID, line, r, item.Timeout)
		if err != nil {
			return StepResult{Ref: step.Ref, Status: state.StatusFailed, ExitCode: res.ExitCode,
				Err: fmt.Errorf("pre_install %q: %w", r.Redact(line), err)}
		}
	}

	if err := runner.Install(ctx, item); err != nil {
		status := state.StatusFailed
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			status = state.StatusCancelled
		}
		return StepResult{Ref: step.Ref, Status: status, Err: redactErr(r, err)}
	}

	for _, line := range item.PostInstall {
		res, err := e.runShell(ctx, runID, stepID, line, r, item.Timeout)
		if err != nil {
			return StepResult{Ref: step.Ref, Status: state.StatusFailed, ExitCode: res.ExitCode,
				Err: fmt.Errorf("post_install %q: %w", r.Redact(line), err)}
		}
	}

	return StepResult{Ref: step.Ref, Status: state.StatusSuccess}
}

// runShell runs a shell line, redacting every captured line before it reaches
// any sink. Redaction happens here, once, so a sink added later cannot leak a
// secret by forgetting to redact.
func (e *Engine) runShell(ctx context.Context, runID, stepID, line string, r *secrets.Redactor, timeout time.Duration) (kexec.Result, error) {
	c := kexec.Shell(line)
	c.Timeout = timeout

	res, err := e.exec.Run(ctx, c, func(stream, out string) {
		e.sink.LogLine(runID, stepID, stream, r.Redact(out))
	})
	if err != nil {
		return res, err
	}
	if res.ExitCode != 0 {
		return res, fmt.Errorf("command %q exited %d: %s",
			r.Redact(line), res.ExitCode, r.Redact(strings.Join(res.Output(), "; ")))
	}
	return res, nil
}
