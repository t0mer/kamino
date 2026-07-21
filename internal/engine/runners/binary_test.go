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

// --- Shell injection is structurally impossible: argv, not /bin/sh -c -----
//
// chmod, mkdir and mv now run through runArgv (argv.go), which builds a
// kexec.Command directly instead of interpolating a string into
// `/bin/sh -c line`. A value containing a shell metacharacter therefore
// reaches the operating system as exactly one argv element — an odd path,
// never a second command.

func TestBinaryPathMetacharactersAreInert(t *testing.T) {
	d, fake, _ := depsWithDownloader(t)

	evil := "/opt/bin/yq; rm -rf /root #"
	err := runners.NewBinary(d).Install(context.Background(), runners.ResolvedItem{
		Ref: "tools/yq", Source: "https://example.com/yq", Path: evil,
	})
	require.NoError(t, err)

	calls := fake.Calls()
	require.Len(t, calls, 3, "chmod, mkdir, mv")

	mv := calls[2]
	assert.Equal(t, "/bin/mv", mv.Path)
	require.Len(t, mv.Args, 3)
	assert.Equal(t, evil, mv.Args[2],
		"the odd path must arrive as a single argv element, not be split by a shell")
}

func TestBinaryRefusesRelativePath(t *testing.T) {
	d, fake, _ := depsWithDownloader(t)

	err := runners.NewBinary(d).Install(context.Background(), runners.ResolvedItem{
		Ref: "tools/yq", Source: "https://example.com/yq", Path: "opt/bin/yq",
	})

	require.Error(t, err)
	assert.Empty(t, fake.CommandLines(), "no command may run once a relative path is rejected")
}

// --- Full command order, by index, not just presence -----------------------
//
// A reviewer noted that asserting each command merely *appears* somewhere in
// the log would not catch a swapped order. Assert the sequence by index.

func TestBinaryInstallCommandOrder(t *testing.T) {
	d, fake, _ := depsWithDownloader(t)

	err := runners.NewBinary(d).Install(context.Background(), runners.ResolvedItem{
		Ref: "tools/yq", Source: "https://example.com/yq",
	})
	require.NoError(t, err)

	calls := fake.Calls()
	require.Len(t, calls, 3)
	assert.Equal(t, "/bin/chmod", calls[0].Path, "step 1 must be chmod, marking the staged file executable")
	assert.Equal(t, "/bin/mkdir", calls[1].Path, "step 2 must be mkdir, ensuring the destination dir exists")
	assert.Equal(t, "/bin/mv", calls[2].Path, "step 3 must be mv, placing the binary at its final path")
}
