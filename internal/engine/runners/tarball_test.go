package runners_test

import (
	"context"
	"os"
	"path/filepath"
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

	// The PATH value itself is written to disk with os.WriteFile, not
	// interpolated into a shell command line — see writePathExport in
	// tarball.go. The move that places it at its real destination is faked
	// (FakeExecutor never touches the real filesystem), so the staged file
	// this test can inspect is still sitting in TempDir afterwards.
	staged := filepath.Join(d.TempDir, "go.profile.sh")
	content, err := os.ReadFile(staged)
	require.NoError(t, err)
	assert.Equal(t, "export PATH=$PATH:/usr/local/go/bin\n", string(content))
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

// --- Shell injection is structurally impossible: argv, not /bin/sh -c -----
//
// rm -rf, mkdir -p and tar -C now run through runArgv (argv.go), which
// builds a kexec.Command directly instead of interpolating a string into
// `/bin/sh -c line`. A value containing a shell metacharacter therefore
// reaches the operating system as exactly one argv element — an odd
// filename, never a second command.

func TestTarballItemIDMetacharactersAreInert(t *testing.T) {
	cases := []struct {
		name string
		ref  string
	}{
		{"semicolon", "dev/go;touch pwned"},
		{"backtick", "dev/go`touch pwned`"},
		{"command substitution", "dev/go$(touch pwned)"},
		{"space", "dev/go touch pwned"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d, fake, _ := depsWithDownloader(t)

			err := runners.NewTarball(d).Install(context.Background(), runners.ResolvedItem{
				Ref: tc.ref, Source: "https://example.com/go.tar.gz", InstallDir: "/usr/local",
			})
			require.NoError(t, err)

			calls := fake.Calls()
			require.Len(t, calls, 3, "rm, mkdir, tar")

			_, name, _ := strings.Cut(tc.ref, "/")
			wantTarget := "/usr/local/" + name

			assert.Equal(t, "/bin/rm", calls[0].Path)
			require.Len(t, calls[0].Args, 2)
			assert.Equal(t, wantTarget, calls[0].Args[1],
				"the odd name must arrive as a single argv element, not be split by a shell")
		})
	}
}

func TestTarballPathExportMetacharactersAreInert(t *testing.T) {
	d, fake, _ := depsWithDownloader(t)

	evil := "/usr/local/go/bin'; rm -rf /home; echo 'pwned"
	err := runners.NewTarball(d).Install(context.Background(), runners.ResolvedItem{
		Ref: "dev/go", Source: "https://example.com/go.tar.gz",
		InstallDir: "/usr/local", PathExport: evil,
	})
	require.NoError(t, err)

	// The value is written to a file by Go itself (os.WriteFile), never
	// interpolated into a shell command line, so it cannot break out of
	// anything — it is just the tail of one line of file content.
	staged := filepath.Join(d.TempDir, "go.profile.sh")
	content, err := os.ReadFile(staged)
	require.NoError(t, err)
	assert.Equal(t, "export PATH=$PATH:"+evil+"\n", string(content))

	// No command executed carries the evil value at all: it never reaches
	// argv, only the file content asserted above.
	for _, line := range fake.CommandLines() {
		assert.NotContains(t, line, "rm -rf /home")
	}
}

func TestTarballRefusesRelativeInstallDir(t *testing.T) {
	d, fake, _ := depsWithDownloader(t)

	err := runners.NewTarball(d).Install(context.Background(), runners.ResolvedItem{
		Ref: "dev/go", Source: "https://example.com/go.tar.gz", InstallDir: "usr/local",
	})

	require.Error(t, err)
	assert.Empty(t, fake.CommandLines(), "no command may run once a relative install dir is rejected")
}

// --- Legitimate install dirs keep working ----------------------------------

func TestTarballAcceptsOptInstallDir(t *testing.T) {
	d, fake, _ := depsWithDownloader(t)

	err := runners.NewTarball(d).Install(context.Background(), runners.ResolvedItem{
		Ref: "dev/go", Source: "https://example.com/go.tar.gz", InstallDir: "/opt",
	})

	require.NoError(t, err)
	calls := fake.Calls()
	require.Len(t, calls, 3)
	assert.Equal(t, []string{"-rf", "/opt/go"}, calls[0].Args)
	assert.Equal(t, []string{"-p", "/opt"}, calls[1].Args)
}

func TestTarballAcceptsNestedInstallDir(t *testing.T) {
	d, fake, _ := depsWithDownloader(t)

	err := runners.NewTarball(d).Install(context.Background(), runners.ResolvedItem{
		Ref: "dev/go", Source: "https://example.com/go.tar.gz", InstallDir: "/opt/mycompany/tools",
	})

	require.NoError(t, err)
	calls := fake.Calls()
	require.Len(t, calls, 3)
	assert.Equal(t, "/opt/mycompany/tools/go", calls[0].Args[1])
	assert.Equal(t, "/opt/mycompany/tools", calls[1].Args[1])
}

// --- Full command order, by index, not just presence -----------------------
//
// A reviewer noted that asserting each command merely *appears* somewhere in
// the log would not catch a swapped order (e.g. mkdir before rm, which would
// recreate the dir and then immediately blow it away). Assert the sequence
// by index instead.

func TestTarballInstallCommandOrder(t *testing.T) {
	d, fake, _ := depsWithDownloader(t)

	err := runners.NewTarball(d).Install(context.Background(), runners.ResolvedItem{
		Ref: "dev/go", Source: "https://example.com/go.tar.gz", InstallDir: "/usr/local",
	})
	require.NoError(t, err)

	calls := fake.Calls()
	require.Len(t, calls, 3)
	assert.Equal(t, "/bin/rm", calls[0].Path, "step 1 must be rm, clearing any stale previous install")
	assert.Equal(t, "/bin/mkdir", calls[1].Path, "step 2 must be mkdir, recreating the install dir")
	assert.Equal(t, "/bin/tar", calls[2].Path, "step 3 must be tar, extracting into the now-fresh dir")
}
