package runners_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/t0mer/kamino/internal/engine/runners"
	kexec "github.com/t0mer/kamino/internal/exec"
)

// fakeSource stands in for the config repo fetcher.
type fakeSource struct {
	files   map[string]string
	fetched []string
	err     error
}

func (f *fakeSource) Fetch(_ context.Context, _ string, path string) ([]byte, error) {
	f.fetched = append(f.fetched, path)
	if f.err != nil {
		return nil, f.err
	}
	body, ok := f.files[path]
	if !ok {
		return nil, errors.New("not found")
	}
	return []byte(body), nil
}

func TestScriptDownloadsRemoteURL(t *testing.T) {
	d, fake, dl := depsWithDownloader(t)
	src := &fakeSource{files: map[string]string{}}

	err := runners.NewScript(d, src, "abc123").Install(context.Background(), runners.ResolvedItem{
		Ref: "tools/docker", Source: "https://get.docker.com",
	})

	require.NoError(t, err)
	assert.Equal(t, []string{"https://get.docker.com"}, dl.Requests())
	assert.Empty(t, src.fetched, "an https source comes from the network, not the config repo")

	joined := strings.Join(fake.CommandLines(), "\n")
	assert.Contains(t, joined, "chmod 0755")
	assert.Contains(t, joined, "/bin/sh")
}

func TestScriptFetchesRepoRelativePathLazily(t *testing.T) {
	d, fake, dl := depsWithDownloader(t)
	src := &fakeSource{files: map[string]string{"scripts/hello.sh": "#!/bin/sh\necho hi\n"}}

	err := runners.NewScript(d, src, "abc123").Install(context.Background(), runners.ResolvedItem{
		Ref: "tools/hello", Source: "scripts/hello.sh",
	})

	require.NoError(t, err)
	assert.Equal(t, []string{"scripts/hello.sh"}, src.fetched)
	assert.Empty(t, dl.Requests(), "a repo-relative script is fetched from the config repo")
	assert.NotEmpty(t, fake.CommandLines())
}

func TestScriptWritesFetchedContentToDisk(t *testing.T) {
	d, _, _ := depsWithDownloader(t)
	body := "#!/bin/sh\necho hi\n"
	src := &fakeSource{files: map[string]string{"scripts/hello.sh": body}}

	err := runners.NewScript(d, src, "abc123").Install(context.Background(), runners.ResolvedItem{
		Ref: "tools/hello", Source: "scripts/hello.sh",
	})

	require.NoError(t, err)
	got, readErr := os.ReadFile(filepath.Join(d.TempDir, "hello.sh"))
	require.NoError(t, readErr)
	assert.Equal(t, body, string(got))
}

func TestScriptMissingRepoFileIsAnError(t *testing.T) {
	d, _, _ := depsWithDownloader(t)
	src := &fakeSource{files: map[string]string{}}

	err := runners.NewScript(d, src, "abc123").Install(context.Background(), runners.ResolvedItem{
		Ref: "tools/hello", Source: "scripts/absent.sh",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "scripts/absent.sh")
}

func TestScriptNonZeroExitIsAnError(t *testing.T) {
	d, fake, _ := depsWithDownloader(t)
	fake.Script("/bin/sh", kexec.Result{ExitCode: 1, Stderr: []string{"install failed"}})
	src := &fakeSource{files: map[string]string{}}

	err := runners.NewScript(d, src, "abc123").Install(context.Background(), runners.ResolvedItem{
		Ref: "tools/docker", Source: "https://get.docker.com",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "install failed")
}

func TestScriptWithoutSourceIsAnError(t *testing.T) {
	d, _, _ := depsWithDownloader(t)

	err := runners.NewScript(d, &fakeSource{}, "abc123").Install(
		context.Background(), runners.ResolvedItem{Ref: "tools/docker"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "source")
}

func TestScriptCheckDelegatesToProbe(t *testing.T) {
	d, fake, _ := depsWithDownloader(t)
	fake.Script("docker --version", kexec.Result{ExitCode: 0, Stdout: []string{"Docker version 27.5"}})

	got, err := runners.NewScript(d, &fakeSource{}, "abc123").Check(
		context.Background(), runners.ResolvedItem{
			Ref: "tools/docker", Check: "docker --version", CheckContains: "Docker version",
		})

	require.NoError(t, err)
	assert.True(t, got)
}

func TestScriptRejectsUnsafeSourceFileName(t *testing.T) {
	d, _, _ := depsWithDownloader(t)
	src := &fakeSource{files: map[string]string{}}

	err := runners.NewScript(d, src, "abc123").Install(context.Background(), runners.ResolvedItem{
		Ref: "tools/evil", Source: "scripts/../..",
	})

	require.Error(t, err)
	assert.Empty(t, src.fetched, "an unsafe target must be rejected before ever fetching the content")
}
