package runners_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/t0mer/kamino/internal/download"
	"github.com/t0mer/kamino/internal/engine/runners"
	kexec "github.com/t0mer/kamino/internal/exec"
)

// depsWithDownloader is like deps (apt_test.go) but also wires a fake
// downloader, needed by any runner that fetches a remote artifact.
func depsWithDownloader(t *testing.T) (runners.Deps, *kexec.FakeExecutor, *download.FakeDownloader) {
	t.Helper()
	fake := kexec.NewFakeExecutor()
	dl := download.NewFakeDownloader()
	return runners.Deps{Exec: fake, Download: dl, TempDir: t.TempDir()}, fake, dl
}

func TestTarballDownloadsAndExtracts(t *testing.T) {
	d, fake, dl := depsWithDownloader(t)

	err := runners.NewTarball(d).Install(context.Background(), runners.ResolvedItem{
		Ref:        "dev/go",
		Source:     "https://go.dev/dl/go1.24.5.linux-amd64.tar.gz",
		SHA256:     "deadbeef",
		InstallDir: "/usr/local",
	})

	require.NoError(t, err)
	assert.Equal(t, []string{"https://go.dev/dl/go1.24.5.linux-amd64.tar.gz"}, dl.Requests())

	joined := strings.Join(fake.CommandLines(), "\n")
	assert.Contains(t, joined, "tar -C /usr/local -xzf")
}

func TestTarballRemovesPreviousInstall(t *testing.T) {
	d, fake, _ := depsWithDownloader(t)

	err := runners.NewTarball(d).Install(context.Background(), runners.ResolvedItem{
		Ref: "dev/go", Source: "https://example.com/go.tar.gz", InstallDir: "/usr/local",
	})

	require.NoError(t, err)
	assert.Contains(t, fake.CommandLines()[0], "rm -rf /usr/local/go",
		"a stale previous install must be cleared or extraction merges two versions")
}

func TestTarballWritesPathExport(t *testing.T) {
	d, fake, _ := depsWithDownloader(t)

	err := runners.NewTarball(d).Install(context.Background(), runners.ResolvedItem{
		Ref: "dev/go", Source: "https://example.com/go.tar.gz",
		InstallDir: "/usr/local", PathExport: "/usr/local/go/bin",
	})

	require.NoError(t, err)
	joined := strings.Join(fake.CommandLines(), "\n")
	assert.Contains(t, joined, "/etc/profile.d/kamino-go.sh")
	assert.Contains(t, joined, "/usr/local/go/bin")
}

func TestTarballDefaultsInstallDir(t *testing.T) {
	d, fake, _ := depsWithDownloader(t)

	err := runners.NewTarball(d).Install(context.Background(), runners.ResolvedItem{
		Ref: "dev/go", Source: "https://example.com/go.tar.gz",
	})

	require.NoError(t, err)
	assert.Contains(t, strings.Join(fake.CommandLines(), "\n"), "tar -C /usr/local")
}

func TestTarballWithoutSourceIsAnError(t *testing.T) {
	d, _, _ := depsWithDownloader(t)

	err := runners.NewTarball(d).Install(context.Background(), runners.ResolvedItem{Ref: "dev/go"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "source")
}

func TestTarballExtractFailureIsAnError(t *testing.T) {
	d, fake, _ := depsWithDownloader(t)
	fake.Script("tar -C", kexec.Result{ExitCode: 2, Stderr: []string{"tar: not in gzip format"}})

	err := runners.NewTarball(d).Install(context.Background(), runners.ResolvedItem{
		Ref: "dev/go", Source: "https://example.com/go.tar.gz",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "gzip")
}

func TestTarballCheckDelegatesToProbe(t *testing.T) {
	d, fake, _ := depsWithDownloader(t)
	fake.Script("go version", kexec.Result{ExitCode: 0, Stdout: []string{"go version go1.24.5 linux/amd64"}})

	got, err := runners.NewTarball(d).Check(context.Background(), runners.ResolvedItem{
		Ref: "dev/go", Check: "go version", CheckContains: "go1.24.5",
	})

	require.NoError(t, err)
	assert.True(t, got)
}

// --- Sanity guard on the rm -rf target (beyond the brief) -----------------
//
// target = <install_dir>/<name> is passed to `rm -rf` as root. A typo in
// the config repo (empty install_dir, install_dir "/", or an item with no
// usable id) must not turn a routine upgrade into `rm -rf /`.

func TestTarballRefusesRootInstallDir(t *testing.T) {
	d, fake, _ := depsWithDownloader(t)

	err := runners.NewTarball(d).Install(context.Background(), runners.ResolvedItem{
		Ref: "dev/go", Source: "https://example.com/go.tar.gz", InstallDir: "/",
	})

	require.Error(t, err)
	assert.Empty(t, fake.CommandLines(), "no command may run once the target is rejected")
}

func TestTarballRefusesEmptyItemID(t *testing.T) {
	d, fake, _ := depsWithDownloader(t)

	// Ref "dev/" -> itemName yields "" after the slash: nothing safe to
	// derive an rm -rf target from.
	err := runners.NewTarball(d).Install(context.Background(), runners.ResolvedItem{
		Ref: "dev/", Source: "https://example.com/go.tar.gz", InstallDir: "/usr/local",
	})

	require.Error(t, err)
	assert.Empty(t, fake.CommandLines())
}

func TestTarballRefusesRefWithNoID(t *testing.T) {
	d, fake, _ := depsWithDownloader(t)

	// An entirely empty Ref: itemName has no slash to cut on, so it returns
	// the (empty) Ref unchanged.
	err := runners.NewTarball(d).Install(context.Background(), runners.ResolvedItem{
		Ref: "", Source: "https://example.com/go.tar.gz", InstallDir: "/usr/local",
	})

	require.Error(t, err)
	assert.Empty(t, fake.CommandLines())
}

func TestTarballRefusesPathTraversalInItemID(t *testing.T) {
	d, fake, _ := depsWithDownloader(t)

	// Ref "dev/../../etc" -> itemName cuts on the first "/" only, yielding
	// "../../etc": a name that would escape the install dir entirely.
	err := runners.NewTarball(d).Install(context.Background(), runners.ResolvedItem{
		Ref: "dev/../../etc", Source: "https://example.com/go.tar.gz", InstallDir: "/usr/local",
	})

	require.Error(t, err)
	assert.Empty(t, fake.CommandLines())
}

func TestTarballAcceptsRefWithNoSlashAsWholeName(t *testing.T) {
	d, fake, _ := depsWithDownloader(t)

	// A ref with no category prefix is unusual but not unsafe: itemName
	// falls back to the whole ref, and that's a fine on-disk name.
	err := runners.NewTarball(d).Install(context.Background(), runners.ResolvedItem{
		Ref: "go", Source: "https://example.com/go.tar.gz", InstallDir: "/usr/local",
	})

	require.NoError(t, err)
	assert.Contains(t, fake.CommandLines()[0], "rm -rf /usr/local/go")
}
