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
	calls := fake.Calls()
	require.Len(t, calls, 1)

	assert.Equal(t, "apt-get", calls[0].Path)
	assert.Equal(t, []string{"install", "-y", "--", "python3.12", "python3.12-venv"}, calls[0].Args)
	assert.Contains(t, calls[0].Env, "DEBIAN_FRONTEND=noninteractive",
		"carried in the environment, not as a shell prefix")
}

// TestAptPackageMetacharactersAreInert pins that a typo'd package name cannot
// become a second command. apt runs via argv, so a ";" is just an odd package
// name that apt will fail to find.
func TestAptPackageMetacharactersAreInert(t *testing.T) {
	d, fake := deps(t)

	err := runners.NewApt(d).Install(context.Background(), runners.ResolvedItem{
		Ref:      "tools/odd",
		Packages: []string{"jq; touch pwned", "`id`", "$(whoami)"},
	})

	require.NoError(t, err)
	calls := fake.Calls()
	require.Len(t, calls, 1, "no extra command may be executed")
	assert.Equal(t, []string{"install", "-y", "--", "jq; touch pwned", "`id`", "$(whoami)"}, calls[0].Args)
	assert.NotEqual(t, "/bin/sh", calls[0].Path)
}

// TestAptLeadingDashPackageIsNotAFlag pins the "--" separator: without it, a
// package name starting with "-" would be parsed by apt-get as a flag.
func TestAptLeadingDashPackageIsNotAFlag(t *testing.T) {
	d, fake := deps(t)

	err := runners.NewApt(d).Install(context.Background(), runners.ResolvedItem{
		Ref: "tools/odd", Packages: []string{"--reinstall"},
	})

	require.NoError(t, err)
	args := fake.Calls()[0].Args
	require.Contains(t, args, "--")
	assert.Less(t, indexOfArg(args, "--"), indexOfArg(args, "--reinstall"),
		"the separator must precede the package list")
}

func indexOfArg(args []string, want string) int {
	for i, a := range args {
		if a == want {
			return i
		}
	}
	return -1
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
