package engine_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/t0mer/kamino/internal/engine"
	"github.com/t0mer/kamino/internal/engine/runners"
	kexec "github.com/t0mer/kamino/internal/exec"
	"github.com/t0mer/kamino/internal/manifest"
	"github.com/t0mer/kamino/internal/plan"
	"github.com/t0mer/kamino/internal/state"
)

// TestRunnerInstallExitCodeReachesStepResult pins the exit code of a command
// that failed inside Runner.Install — not inside a pre_install or post_install
// line.
//
// That distinction is the whole point: an earlier fix threaded the exit code
// through the sink interface but only ever populated it for pre/post_install,
// so the primary failure mode — `apt-get install` returning 100 — still
// persisted 0 and left the run history unable to say what went wrong.
func TestRunnerInstallExitCodeReachesStepResult(t *testing.T) {
	fake := kexec.NewFakeExecutor()
	fake.Script("apt-get", kexec.Result{ExitCode: 100, Stderr: []string{"E: Unable to locate package nope"}})

	deps := runners.Deps{Exec: fake, TempDir: t.TempDir()}
	apt := runners.NewApt(deps)

	eng := engine.New(
		fake,
		func(manifest.ItemType) (runners.Runner, bool) { return apt, true },
		nil,
		engine.Options{Arch: "amd64"},
	)

	it := manifest.Item{
		ID: "nope", Name: "nope", CategoryID: "tools",
		Type: manifest.ItemApt, Packages: []string{"nope"},
	}
	p := &plan.Plan{ProfileID: "t", Arch: "amd64", Steps: []plan.Step{
		{Ref: it.Ref(), Name: it.Name, Type: string(it.Type), Item: it},
	}}

	got, err := eng.Run(context.Background(), p, "run-1")

	require.NoError(t, err)
	require.Len(t, got.Steps, 1)
	assert.Equal(t, state.StatusFailed, got.Steps[0].Status)
	assert.Equal(t, 100, got.Steps[0].ExitCode,
		"a command that failed inside Install must report its exit code, not 0")
}
