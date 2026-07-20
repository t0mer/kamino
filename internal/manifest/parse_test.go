package manifest_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/t0mer/kamino/internal/manifest"
)

func read(t *testing.T, rel string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "testdata", rel))
	require.NoError(t, err)
	return b
}

func TestParseManifest(t *testing.T) {
	m, err := manifest.ParseManifest(read(t, "config/manifest.yaml"))
	require.NoError(t, err)

	assert.Equal(t, 1, m.Schema)
	assert.Equal(t, "kamino-testdata", m.Name)
	assert.True(t, m.Defaults.AptUpdateBeforeRun)
	assert.Equal(t, 15*time.Minute, m.Defaults.Timeout)
	assert.Len(t, m.Categories, 3)
	assert.Len(t, m.Profiles, 3)
}

func TestParseCategoryPopulatesRefs(t *testing.T) {
	c, err := manifest.ParseCategory(read(t, "config/categories/dev.yaml"))
	require.NoError(t, err)

	require.Len(t, c.Items, 3)
	assert.Equal(t, "dev", c.ID)
	assert.Equal(t, 10, c.Order)

	got := c.Items[0]
	assert.Equal(t, "dev/go", got.Ref())
	assert.Equal(t, manifest.ItemTarball, got.Type)
	assert.Equal(t, "1.24.5", got.Version)
	assert.Equal(t, "/usr/local", got.InstallDir)
	assert.Equal(t, "go version", got.Check)
	assert.Equal(t, "https://go.dev/dl/go{version}.linux-amd64.tar.gz", got.Source["amd64"])
}

func TestParseCategoryDependsOn(t *testing.T) {
	c, err := manifest.ParseCategory(read(t, "config/categories/dev.yaml"))
	require.NoError(t, err)
	assert.Equal(t, []string{"dev/python"}, c.Items[2].DependsOn)
}

func TestParseProfile(t *testing.T) {
	p, err := manifest.ParseProfile(read(t, "config/profiles/production.yaml"))
	require.NoError(t, err)

	assert.Equal(t, "production", p.ID)
	assert.Equal(t, []string{"tools/*", "network/cloudflared"}, p.Include)
	assert.Equal(t, []string{"dev/*"}, p.Exclude)
	assert.Equal(t, "27.5", p.Overrides["tools/docker"].Version)
}

func TestParseRejectsUnknownFields(t *testing.T) {
	_, err := manifest.ParseCategory(read(t, "invalid/unknown-field.yaml"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "verzion")
}

func TestSourceAcceptsBareString(t *testing.T) {
	c, err := manifest.ParseCategory([]byte(`
id: tools
name: Tools
items:
  - id: docker
    name: Docker
    type: script
    source: "https://get.docker.com"
`))
	require.NoError(t, err)
	assert.Equal(t, "https://get.docker.com", c.Items[0].Source["amd64"])
	assert.Equal(t, "https://get.docker.com", c.Items[0].Source["arm64"])
}
