package runners_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/t0mer/kamino/internal/engine/runners"
	kexec "github.com/t0mer/kamino/internal/exec"
)

func TestPipInstallsPackages(t *testing.T) {
	d, fake := deps(t)

	err := runners.NewPip(d).Install(context.Background(), runners.ResolvedItem{
		Ref: "dev/pypi-base", Packages: []string{"loguru", "requests"},
	})

	require.NoError(t, err)
	calls := fake.Calls()
	require.Len(t, calls, 1)
	assert.Equal(t, "python3", calls[0].Path)
	assert.Equal(t, []string{"-m", "pip", "install", "--break-system-packages", "loguru", "requests"}, calls[0].Args)
}

func TestPipUsesDeclaredPythonVersion(t *testing.T) {
	d, fake := deps(t)

	err := runners.NewPip(d).Install(context.Background(), runners.ResolvedItem{
		Ref: "dev/pypi-base", Python: "3.12", Packages: []string{"loguru"},
	})

	require.NoError(t, err)
	assert.Equal(t, "python3.12", fake.Calls()[0].Path)
}

func TestPipBreaksSystemPackages(t *testing.T) {
	d, fake := deps(t)

	err := runners.NewPip(d).Install(context.Background(), runners.ResolvedItem{
		Ref: "dev/pypi-base", Packages: []string{"loguru"},
	})

	require.NoError(t, err)
	assert.Contains(t, fake.Calls()[0].Args, "--break-system-packages",
		"Ubuntu 24.04 refuses system-wide pip installs without it")
}

func TestPipWithoutPackagesIsAnError(t *testing.T) {
	d, fake := deps(t)

	err := runners.NewPip(d).Install(context.Background(), runners.ResolvedItem{Ref: "dev/pypi-base"})

	require.Error(t, err)
	assert.Empty(t, fake.Calls(), "no command may run once the missing-packages check fails")
}

func TestPipInstallFailureIsAnError(t *testing.T) {
	d, fake := deps(t)
	fake.Script("pip install", kexec.Result{ExitCode: 1, Stderr: []string{"ERROR: No matching distribution"}})

	err := runners.NewPip(d).Install(context.Background(), runners.ResolvedItem{
		Ref: "dev/pypi-base", Packages: []string{"nope"},
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "No matching distribution")
}

func TestPipCheckDelegatesToProbe(t *testing.T) {
	d, fake := deps(t)
	fake.Script("pip show loguru", kexec.Result{ExitCode: 0, Stdout: []string{"Name: loguru"}})

	got, err := runners.NewPip(d).Check(context.Background(), runners.ResolvedItem{
		Ref: "dev/pypi-base", Check: "python3.12 -m pip show loguru", CheckContains: "Name: loguru",
	})

	require.NoError(t, err)
	assert.True(t, got)
}

// --- Each package is its own argv element, never a joined string ----------

func TestPipPackageWithSpaceOrSemicolonIsOneInertArgument(t *testing.T) {
	d, fake := deps(t)

	evil := "loguru; rm -rf /root #"
	err := runners.NewPip(d).Install(context.Background(), runners.ResolvedItem{
		Ref: "dev/pypi-base", Packages: []string{evil, "requests"},
	})

	require.NoError(t, err)
	args := fake.Calls()[0].Args
	require.Len(t, args, 6, "-m pip install --break-system-packages <evil> requests")
	assert.Equal(t, evil, args[4], "the odd package name must arrive as a single argv element")
	assert.Equal(t, "requests", args[5])
}
