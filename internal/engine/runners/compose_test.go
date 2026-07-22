package runners_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/t0mer/kamino/internal/engine/runners"
	kexec "github.com/t0mer/kamino/internal/exec"
)

// fakeSecrets is an in-memory SecretSource for tests.
type fakeSecrets map[string]string

func (f fakeSecrets) Get(name string) (string, bool) { v, ok := f[name]; return v, ok }

const composeYAML = "services:\n  app:\n    image: hello\n"

func composeItem() runners.ResolvedItem {
	return runners.ResolvedItem{
		Ref:   "containers/monitoring",
		Name:  "Monitoring stack",
		Path:  "stacks/monitoring",
		Files: []string{"docker-compose.yaml", "prometheus.yml"},
	}
}

func TestComposeMaterialisesFilesAndRunsUp(t *testing.T) {
	d, fake, _ := depsWithDownloader(t)
	src := &fakeSource{files: map[string]string{
		"stacks/monitoring/docker-compose.yaml": composeYAML,
		"stacks/monitoring/prometheus.yml":      "global: {}\n",
	}}

	err := runners.NewCompose(d, src, "abc123", fakeSecrets{}).Install(context.Background(), composeItem())
	require.NoError(t, err)

	// Both declared files fetched at the item's Path.
	assert.ElementsMatch(t,
		[]string{"stacks/monitoring/docker-compose.yaml", "stacks/monitoring/prometheus.yml"},
		src.fetched)

	// Materialised on disk under a per-item stack dir.
	got, readErr := os.ReadFile(filepath.Join(d.TempDir, "monitoring", "docker-compose.yaml"))
	require.NoError(t, readErr)
	assert.Equal(t, composeYAML, string(got))

	// Exactly one command: docker compose ... up -d, pointed at the compose file.
	calls := fake.Calls()
	require.Len(t, calls, 1)
	assert.Equal(t, "docker", calls[0].Path)
	line := calls[0].Line()
	assert.Contains(t, line, "compose")
	assert.Contains(t, line, "up -d")
	assert.Contains(t, line, filepath.Join(d.TempDir, "monitoring", "docker-compose.yaml"))
	assert.Contains(t, line, "-p monitoring")
}

func TestComposeInjectsSecretsAsEnvNotArgv(t *testing.T) {
	d, fake, _ := depsWithDownloader(t)
	src := &fakeSource{files: map[string]string{"stacks/monitoring/docker-compose.yaml": composeYAML}}
	it := composeItem()
	it.Files = []string{"docker-compose.yaml"}
	it.Secrets = []string{"CF_TUNNEL_TOKEN"}

	err := runners.NewCompose(d, src, "abc123", fakeSecrets{"CF_TUNNEL_TOKEN": "sup3rs3cret"}).
		Install(context.Background(), it)
	require.NoError(t, err)

	calls := fake.Calls()
	require.Len(t, calls, 1)
	assert.Contains(t, calls[0].Env, "CF_TUNNEL_TOKEN=sup3rs3cret",
		"the secret must reach the process as an env var")
	assert.NotContains(t, calls[0].Line(), "sup3rs3cret",
		"the secret must never reach argv, where ps and /proc expose it")
}

func TestComposeMissingSecretIsAnError(t *testing.T) {
	d, fake, _ := depsWithDownloader(t)
	src := &fakeSource{files: map[string]string{"stacks/monitoring/docker-compose.yaml": composeYAML}}
	it := composeItem()
	it.Files = []string{"docker-compose.yaml"}
	it.Secrets = []string{"CF_TUNNEL_TOKEN"}

	err := runners.NewCompose(d, src, "abc123", fakeSecrets{}).Install(context.Background(), it)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "CF_TUNNEL_TOKEN")
	assert.Empty(t, fake.Calls(), "no command may run when a declared secret is missing")
}

func TestComposeRequiresPathAndFiles(t *testing.T) {
	d, _, _ := depsWithDownloader(t)
	src := &fakeSource{files: map[string]string{}}

	noPath := composeItem()
	noPath.Path = ""
	err := runners.NewCompose(d, src, "abc123", fakeSecrets{}).Install(context.Background(), noPath)
	require.Error(t, err)
	assert.Empty(t, src.fetched, "nothing is fetched when path is missing")

	noFiles := composeItem()
	noFiles.Files = nil
	err = runners.NewCompose(d, src, "abc123", fakeSecrets{}).Install(context.Background(), noFiles)
	require.Error(t, err)
}

func TestComposeNoComposeFileAmongFilesIsAnError(t *testing.T) {
	d, _, _ := depsWithDownloader(t)
	src := &fakeSource{files: map[string]string{"stacks/monitoring/prometheus.yml": "global: {}\n"}}
	it := composeItem()
	it.Files = []string{"prometheus.yml"}

	err := runners.NewCompose(d, src, "abc123", fakeSecrets{}).Install(context.Background(), it)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "docker-compose")
}

func TestComposeRejectsTraversalFileEntry(t *testing.T) {
	d, _, _ := depsWithDownloader(t)
	src := &fakeSource{files: map[string]string{}}
	it := composeItem()
	it.Files = []string{"../../etc/evil.yaml"}

	err := runners.NewCompose(d, src, "abc123", fakeSecrets{}).Install(context.Background(), it)
	require.Error(t, err)
	assert.Empty(t, src.fetched, "an unsafe file entry must be rejected before fetching")
}

func TestComposeFetchFailureNamesThePath(t *testing.T) {
	d, _, _ := depsWithDownloader(t)
	src := &fakeSource{files: map[string]string{}, err: errors.New("404")}
	it := composeItem()
	it.Files = []string{"docker-compose.yaml"}

	err := runners.NewCompose(d, src, "abc123", fakeSecrets{}).Install(context.Background(), it)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "stacks/monitoring/docker-compose.yaml")
}

func TestComposeCheckDefaultsToFalse(t *testing.T) {
	d, _, _ := depsWithDownloader(t)
	got, err := runners.NewCompose(d, &fakeSource{}, "abc123", fakeSecrets{}).
		Check(context.Background(), composeItem())
	require.NoError(t, err)
	assert.False(t, got, "with no check declared, up -d handles idempotency")
}

func TestComposeCheckHonoursExplicitProbe(t *testing.T) {
	d, fake, _ := depsWithDownloader(t)
	fake.Script("docker compose ls", kexec.Result{ExitCode: 0, Stdout: []string{"monitoring running"}})
	it := composeItem()
	it.Check = "docker compose ls"
	it.CheckContains = "monitoring"

	got, err := runners.NewCompose(d, &fakeSource{}, "abc123", fakeSecrets{}).
		Check(context.Background(), it)
	require.NoError(t, err)
	assert.True(t, got)
}
