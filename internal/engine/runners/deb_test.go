package runners_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/t0mer/kamino/internal/engine/runners"
	kexec "github.com/t0mer/kamino/internal/exec"
)

func TestDebDownloadsAndInstalls(t *testing.T) {
	d, fake, dl := depsWithDownloader(t)

	err := runners.NewDeb(d).Install(context.Background(), runners.ResolvedItem{
		Ref:    "network/cloudflared",
		Source: "https://example.com/cloudflared.deb",
		SHA256: "deadbeef",
	})

	require.NoError(t, err)
	assert.Equal(t, []string{"https://example.com/cloudflared.deb"}, dl.Requests())

	calls := fake.Calls()
	require.NotEmpty(t, calls)
	assert.Equal(t, "/usr/bin/dpkg", calls[0].Path, "the distro dpkg is invoked by absolute path, not PATH lookup")
	assert.Equal(t, "-i", calls[0].Args[0])
}

func TestDebFixesBrokenDependencies(t *testing.T) {
	d, fake, _ := depsWithDownloader(t)

	err := runners.NewDeb(d).Install(context.Background(), runners.ResolvedItem{
		Ref: "network/cloudflared", Source: "https://example.com/cloudflared.deb",
	})

	require.NoError(t, err)
	calls := fake.Calls()
	require.Len(t, calls, 2, "dpkg -i, then the apt-get fix-up pass")

	dpkg := calls[0]
	assert.Equal(t, "/usr/bin/dpkg", dpkg.Path, "step 1 must be dpkg -i, installing the downloaded package")

	fixup := calls[1]
	assert.Equal(t, "/usr/bin/apt-get", fixup.Path, "step 2 must be apt-get install -f -y; dpkg does not resolve dependencies")
	assert.Equal(t, []string{"install", "-f", "-y"}, fixup.Args)
}

func TestDebSetsDebianFrontendNonInteractive(t *testing.T) {
	d, fake, _ := depsWithDownloader(t)

	err := runners.NewDeb(d).Install(context.Background(), runners.ResolvedItem{
		Ref: "network/cloudflared", Source: "https://example.com/cloudflared.deb",
	})

	require.NoError(t, err)
	calls := fake.Calls()
	require.Len(t, calls, 2)
	assert.Contains(t, calls[0].Env, "DEBIAN_FRONTEND=noninteractive",
		"dpkg's env must carry the flag, not a shell string prefix")
	assert.Contains(t, calls[1].Env, "DEBIAN_FRONTEND=noninteractive",
		"the apt-get fix-up's env must carry the flag too")
}

func TestDebInstallFailureIsAnError(t *testing.T) {
	d, fake, _ := depsWithDownloader(t)
	fake.Script("dpkg -i", kexec.Result{ExitCode: 1, Stderr: []string{"dpkg: error processing archive"}})

	err := runners.NewDeb(d).Install(context.Background(), runners.ResolvedItem{
		Ref: "network/cloudflared", Source: "https://example.com/cloudflared.deb",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "error processing archive")
}

func TestDebFixupFailureIsAnError(t *testing.T) {
	d, fake, _ := depsWithDownloader(t)
	fake.Script("apt-get install -f -y", kexec.Result{ExitCode: 1, Stderr: []string{"unmet dependencies"}})

	err := runners.NewDeb(d).Install(context.Background(), runners.ResolvedItem{
		Ref: "network/cloudflared", Source: "https://example.com/cloudflared.deb",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unmet dependencies")
}

func TestDebWithoutSourceIsAnError(t *testing.T) {
	d, _, _ := depsWithDownloader(t)

	err := runners.NewDeb(d).Install(context.Background(), runners.ResolvedItem{Ref: "network/cloudflared"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "source")
}

func TestDebCheckDelegatesToProbe(t *testing.T) {
	d, fake, _ := depsWithDownloader(t)
	fake.Script("cloudflared --version", kexec.Result{ExitCode: 0, Stdout: []string{"cloudflared version 2024.1.0"}})

	got, err := runners.NewDeb(d).Check(context.Background(), runners.ResolvedItem{
		Ref: "network/cloudflared", Check: "cloudflared --version", CheckContains: "2024.1.0",
	})

	require.NoError(t, err)
	assert.True(t, got)
}

// --- Shell injection is structurally impossible: argv, not /bin/sh -c -----

func TestDebDownloadedPathMetacharactersAreInert(t *testing.T) {
	d, fake, dl := depsWithDownloader(t)
	dl.Content("https://example.com/pkg.deb", "binary content")

	err := runners.NewDeb(d).Install(context.Background(), runners.ResolvedItem{
		Ref: "network/pkg; touch pwned #", Source: "https://example.com/pkg.deb",
	})

	require.NoError(t, err)
	calls := fake.Calls()
	require.Len(t, calls, 2)
	// The staged .deb path is derived from the item id; whatever it is, it
	// must arrive at dpkg as a single argv element, not be parsed by a shell.
	require.Len(t, calls[0].Args, 2)
	assert.Equal(t, "-i", calls[0].Args[0])
}
