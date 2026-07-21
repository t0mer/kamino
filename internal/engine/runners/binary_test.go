package runners_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/t0mer/kamino/internal/engine/runners"
	kexec "github.com/t0mer/kamino/internal/exec"
)

func TestBinaryInstallsToUsrLocalBin(t *testing.T) {
	d, fake, dl := depsWithDownloader(t)

	err := runners.NewBinary(d).Install(context.Background(), runners.ResolvedItem{
		Ref: "tools/yq", Source: "https://example.com/yq_linux_amd64", SHA256: "cafebabe",
	})

	require.NoError(t, err)
	assert.Equal(t, []string{"https://example.com/yq_linux_amd64"}, dl.Requests())

	joined := strings.Join(fake.CommandLines(), "\n")
	assert.Contains(t, joined, "chmod 0755")
	assert.Contains(t, joined, "/usr/local/bin/yq")
}

func TestBinaryHonoursExplicitPath(t *testing.T) {
	d, fake, _ := depsWithDownloader(t)

	err := runners.NewBinary(d).Install(context.Background(), runners.ResolvedItem{
		Ref: "tools/yq", Source: "https://example.com/yq", Path: "/opt/bin/yq",
	})

	require.NoError(t, err)
	assert.Contains(t, strings.Join(fake.CommandLines(), "\n"), "/opt/bin/yq")
}

func TestBinaryWithoutSourceIsAnError(t *testing.T) {
	d, _, _ := depsWithDownloader(t)

	err := runners.NewBinary(d).Install(context.Background(), runners.ResolvedItem{Ref: "tools/yq"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "source")
}

func TestBinaryMoveFailureIsAnError(t *testing.T) {
	d, fake, _ := depsWithDownloader(t)
	fake.Script("mv -f", kexec.Result{ExitCode: 1, Stderr: []string{"Permission denied"}})

	err := runners.NewBinary(d).Install(context.Background(), runners.ResolvedItem{
		Ref: "tools/yq", Source: "https://example.com/yq",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "Permission denied")
}

func TestBinaryCheckDelegatesToProbe(t *testing.T) {
	d, fake, _ := depsWithDownloader(t)
	fake.Script("yq --version", kexec.Result{ExitCode: 0, Stdout: []string{"yq (https://github.com/mikefarah/yq/) version v4.44.0"}})

	got, err := runners.NewBinary(d).Check(context.Background(), runners.ResolvedItem{
		Ref: "tools/yq", Check: "yq --version", CheckContains: "v4.44.0",
	})

	require.NoError(t, err)
	assert.True(t, got)
}
