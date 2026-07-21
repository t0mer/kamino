package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"github.com/t0mer/kamino/internal/download"
	"github.com/t0mer/kamino/internal/engine"
	"github.com/t0mer/kamino/internal/engine/runners"
	kexec "github.com/t0mer/kamino/internal/exec"
	"github.com/t0mer/kamino/internal/manifest"
	"github.com/t0mer/kamino/internal/plan"
	"github.com/t0mer/kamino/internal/secrets"
	"github.com/t0mer/kamino/internal/state"
	"github.com/t0mer/kamino/internal/sysinfo"
)

// KeepRuns is how many runs are retained in the local database.
const KeepRuns = 50

func newApplyCmd() *cobra.Command {
	var (
		profileID       string
		arch            string
		continueOnError bool
		dryRun          bool
		yes             bool
		verbose         bool
		secretFlags     []string
	)

	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Execute a profile's install plan on this host",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			out := cmd.OutOrStdout()

			// Every secret must be registered on the store before the engine is
			// built: engine.Run snapshots the store into a redactor at the start
			// of the run (see internal/engine.Run's doc comment), so a Set call
			// after that point would leave the new secret's value unredacted in
			// every log line and step error the run persists.
			store := secrets.New()
			for _, kv := range secretFlags {
				key, value, found := strings.Cut(kv, "=")
				if !found || key == "" {
					return fmt.Errorf("invalid --secret %q: expected KEY=VALUE", kv)
				}
				store.Set(key, value)
			}

			resolved, err := loadConfig(ctx)
			if err != nil {
				return err
			}
			if problems := manifest.Validate(resolved); problems.HasErrors() {
				return fmt.Errorf("config repo is invalid:\n%s", problems.Error())
			}

			profile, err := findProfile(resolved, profileID)
			if err != nil {
				return err
			}

			built, err := plan.Build(resolved, profile, arch)
			if err != nil {
				return err
			}

			// Fail before touching the machine if a declared secret has no
			// value: discovering it mid-run leaves a half-provisioned host.
			var declared []string
			for _, s := range built.Steps {
				declared = append(declared, s.Item.Secrets...)
			}
			if missing := secrets.Missing(declared, store); len(missing) > 0 {
				return fmt.Errorf("missing required secret(s): %s (pass --secret KEY=VALUE)",
					strings.Join(missing, ", "))
			}

			if dryRun {
				renderPlanText(out, built)
				fmt.Fprintln(out, "\ndry run: nothing was installed")
				return nil
			}

			info, err := sysinfo.Detect()
			if err != nil {
				return err
			}
			if err := info.RequireUbuntuRoot(); err != nil {
				return err
			}

			if !yes {
				fmt.Fprintf(out, "About to run %d step(s) as root from %s (SHA %s).\n",
					len(built.Steps), resolved.Manifest.Name, built.ConfigSHA)
				return fmt.Errorf("refusing to proceed without --yes")
			}

			return execute(cmd, built, resolved, store, continueOnError, verbose)
		},
	}

	cmd.Flags().StringVar(&profileID, "profile", "", "profile to apply (required)")
	cmd.Flags().StringVar(&arch, "arch", runtime.GOARCH, "target architecture")
	cmd.Flags().BoolVar(&continueOnError, "continue-on-error", false, "keep going after a failed step")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print the plan and exit without installing")
	cmd.Flags().BoolVar(&yes, "yes", false, "proceed without confirmation")
	cmd.Flags().BoolVar(&verbose, "verbose", false, "stream command output")
	cmd.Flags().StringArrayVar(&secretFlags, "secret", nil, "secret value as KEY=VALUE (repeatable)")
	_ = cmd.MarkFlagRequired("profile")
	return cmd
}

// runInTempDir creates a per-run temp directory, invokes fn with its path, and
// always removes the directory before returning — whether fn succeeds, fails,
// or the caller's context is cancelled mid-run.
//
// Runners materialise downloaded artefacts (tarballs, .deb packages, fetched
// scripts) into this directory, and nothing else on the apply path ever
// cleans them up. A root-executable script left behind after a run matters,
// so the removal is unconditional: it happens via defer, which still fires
// when fn returns an error. The returned path is only useful for logging or
// tests — by the time this function returns to its caller, the directory it
// names is already gone.
func runInTempDir(fn func(tempDir string) error) (string, error) {
	tempDir, err := os.MkdirTemp("", "kamino-run-*")
	if err != nil {
		return "", fmt.Errorf("creating run temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	return tempDir, fn(tempDir)
}

// execute runs the plan, persisting state and streaming progress.
func execute(cmd *cobra.Command, built *plan.Plan, resolved *manifest.Resolved,
	store *secrets.Store, continueOnError, verbose bool) error {

	ctx := cmd.Context()
	out := cmd.OutOrStdout()

	db, err := state.Open(filepath.Join(flags.dataDir, "kamino.db"))
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	runID := uuid.NewString()
	started := time.Now().UTC()
	if err := db.CreateRun(state.Run{
		ID: runID, StartedAt: started, Profile: built.ProfileID,
		ConfigSHA: built.ConfigSHA, Status: state.StatusRunning,
	}); err != nil {
		return err
	}

	stepIDs := map[string]string{}
	names := map[string]string{}
	for _, s := range built.Steps {
		id := uuid.NewString()
		stepIDs[s.Ref] = id
		names[s.Ref] = s.Name
		if err := db.CreateStep(state.Step{
			ID: id, RunID: runID, ItemRef: s.Ref, Name: s.Name, Status: state.StatusPending,
		}); err != nil {
			return err
		}
	}

	var summary engine.Summary
	_, tempDirErr := runInTempDir(func(tempDir string) error {
		deps := runners.Deps{
			Exec:     kexec.NewRealExecutor(),
			Download: download.NewHTTPDownloader(nil),
			TempDir:  tempDir,
		}

		eng := engine.New(
			deps.Exec,
			engine.NewRegistry(deps, configSource(ctx), built.ConfigSHA),
			engine.NewMultiSink(
				state.NewSink(db, stepIDs),
				newTermSink(out, len(built.Steps), names, verbose),
			),
			engine.Options{
				ContinueOnError: continueOnError,
				AptUpdate:       resolved.Manifest.Defaults.AptUpdateBeforeRun,
				Arch:            built.Arch,
				Defaults:        resolved.Manifest.Defaults,
				Secrets:         store,
			},
		)

		var runErr error
		summary, runErr = eng.Run(ctx, built, runID)
		return runErr
	})
	if tempDirErr != nil {
		_ = db.FinishRun(runID, state.StatusFailed, time.Now().UTC())
		return tempDirErr
	}

	if err := db.FinishRun(runID, summary.Status, time.Now().UTC()); err != nil {
		return err
	}
	if err := db.Prune(KeepRuns); err != nil {
		return err
	}

	fmt.Fprintf(out, "\nrun %s: %s in %s\n", runID, summary.Status, time.Since(started).Round(time.Second))

	if summary.Status != state.StatusSuccess {
		for _, s := range summary.Steps {
			if s.Err != nil {
				fmt.Fprintf(out, "  %s: %v\n", s.Ref, s.Err)
			}
		}
		return fmt.Errorf("run finished with status %s", summary.Status)
	}
	return nil
}
