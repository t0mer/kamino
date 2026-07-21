package runners_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/t0mer/kamino/internal/engine/runners"
	kexec "github.com/t0mer/kamino/internal/exec"
)

func deps(t *testing.T) (runners.Deps, *kexec.FakeExecutor) {
	t.Helper()
	fake := kexec.NewFakeExecutor()
	return runners.Deps{Exec: fake, TempDir: t.TempDir()}, fake
}

func TestAptCheckReportsInstalledOnMatch(t *testing.T) {
	d, fake := deps(t)
	fake.Script("jq --version", kexec.Result{ExitCode: 0, Stdout: []string{"jq-1.7"}})

	got, err := runners.NewApt(d).Check(context.Background(), runners.ResolvedItem{
		Ref: "tools/jq", Check: "jq --version", CheckContains: "jq-",
	})

	require.NoError(t, err)
	assert.True(t, got)
}

func TestAptCheckReportsNotInstalledOnMissingBinary(t *testing.T) {
	d, fake := deps(t)
	// 127 is what a shell returns for "command not found".
	fake.Script("jq --version", kexec.Result{ExitCode: 127, Stderr: []string{"jq: not found"}})

	got, err := runners.NewApt(d).Check(context.Background(), runners.ResolvedItem{
		Ref: "tools/jq", Check: "jq --version", CheckContains: "jq-",
	})

	require.NoError(t, err, "a failing probe is an answer, not an error")
	assert.False(t, got)
}

func TestAptCheckReportsNotInstalledOnOutputMismatch(t *testing.T) {
	d, fake := deps(t)
	fake.Script("python3.12 --version", kexec.Result{ExitCode: 0, Stdout: []string{"Python 3.11.2"}})

	got, err := runners.NewApt(d).Check(context.Background(), runners.ResolvedItem{
		Ref: "dev/python", Check: "python3.12 --version", CheckContains: "Python 3.12",
	})

	require.NoError(t, err)
	assert.False(t, got, "wrong version installed means the item is not satisfied")
}

func TestAptCheckWithoutProbeIsNotInstalled(t *testing.T) {
	d, _ := deps(t)

	got, err := runners.NewApt(d).Check(context.Background(), runners.ResolvedItem{Ref: "tools/jq"})

	require.NoError(t, err)
	assert.False(t, got)
}

func TestAptCheckIgnoresContainsWhenUnset(t *testing.T) {
	d, fake := deps(t)
	fake.Script("jq --version", kexec.Result{ExitCode: 0, Stdout: []string{"anything"}})

	got, err := runners.NewApt(d).Check(context.Background(), runners.ResolvedItem{
		Ref: "tools/jq", Check: "jq --version",
	})

	require.NoError(t, err)
	assert.True(t, got, "exit 0 with no check_contains means installed")
}

func TestAptInstallRunsNonInteractiveInstall(t *testing.T) {
	d, fake := deps(t)

	err := runners.NewApt(d).Install(context.Background(), runners.ResolvedItem{
		Ref: "dev/python", Packages: []string{"python3.12", "python3.12-venv"},
	})

	require.NoError(t, err)
	require.Len(t, fake.CommandLines(), 1)
	line := fake.CommandLines()[0]
	assert.Contains(t, line, "DEBIAN_FRONTEND=noninteractive")
	assert.Contains(t, line, "apt-get install -y")
	assert.Contains(t, line, "python3.12 python3.12-venv")
}

func TestAptInstallAddsRepoFirst(t *testing.T) {
	d, fake := deps(t)

	err := runners.NewApt(d).Install(context.Background(), runners.ResolvedItem{
		Ref: "dev/python", Repo: "ppa:deadsnakes/ppa", Packages: []string{"python3.12"},
	})

	require.NoError(t, err)
	lines := fake.CommandLines()
	require.Len(t, lines, 3)
	assert.Contains(t, lines[0], "add-apt-repository -y ppa:deadsnakes/ppa")
	assert.Contains(t, lines[1], "apt-get update", "a new repo needs an index refresh before install")
	assert.Contains(t, lines[2], "apt-get install -y")
}

func TestAptInstallFailureIsAnError(t *testing.T) {
	d, fake := deps(t)
	fake.Script("apt-get install", kexec.Result{ExitCode: 100, Stderr: []string{"E: Unable to locate package"}})

	err := runners.NewApt(d).Install(context.Background(), runners.ResolvedItem{
		Ref: "tools/nope", Packages: []string{"nope"},
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "Unable to locate package")
}

func TestAptInstallWithoutPackagesIsAnError(t *testing.T) {
	d, _ := deps(t)

	err := runners.NewApt(d).Install(context.Background(), runners.ResolvedItem{Ref: "tools/x"})

	require.Error(t, err)
}
