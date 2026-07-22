package engine_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/t0mer/kamino/internal/engine"
	"github.com/t0mer/kamino/internal/manifest"
	"github.com/t0mer/kamino/internal/secrets"
)

func TestResolveExpandsVersionInSourceAndCheck(t *testing.T) {
	it := manifest.Item{
		ID: "go", Name: "Go", CategoryID: "dev", Type: manifest.ItemTarball,
		Version:       "1.24.5",
		Source:        manifest.Source{"amd64": "https://go.dev/dl/go{version}.linux-amd64.tar.gz"},
		SHA256:        manifest.Source{"amd64": "deadbeef"},
		Check:         "go version",
		CheckContains: "go{version}",
	}

	got, err := engine.Resolve(it, "amd64", manifest.Defaults{}, secrets.New())

	require.NoError(t, err)
	assert.Equal(t, "dev/go", got.Ref)
	assert.Equal(t, "https://go.dev/dl/go1.24.5.linux-amd64.tar.gz", got.Source)
	assert.Equal(t, "deadbeef", got.SHA256)
	assert.Equal(t, "go1.24.5", got.CheckContains)
}

func TestResolvePicksArchSource(t *testing.T) {
	it := manifest.Item{
		ID: "go", CategoryID: "dev", Type: manifest.ItemTarball, Version: "1.24.5",
		Source: manifest.Source{
			"amd64": "https://example.com/amd64.tar.gz",
			"arm64": "https://example.com/arm64.tar.gz",
		},
	}

	got, err := engine.Resolve(it, "arm64", manifest.Defaults{}, secrets.New())

	require.NoError(t, err)
	assert.Equal(t, "https://example.com/arm64.tar.gz", got.Source)
}

func TestResolveExpandsPackages(t *testing.T) {
	it := manifest.Item{
		ID: "python", CategoryID: "dev", Type: manifest.ItemApt, Version: "3.12",
		Packages: []string{"python{version}", "python{version}-venv"},
	}

	got, err := engine.Resolve(it, "amd64", manifest.Defaults{}, secrets.New())

	require.NoError(t, err)
	assert.Equal(t, []string{"python3.12", "python3.12-venv"}, got.Packages)
}

func TestResolveExpandsSecretsInPostInstall(t *testing.T) {
	s := secrets.New()
	s.Set("CF_TUNNEL_TOKEN", "tok3n")
	it := manifest.Item{
		ID: "cloudflared", CategoryID: "network", Type: manifest.ItemDeb,
		PostInstall: []string{"cloudflared service install {secret:CF_TUNNEL_TOKEN}"},
	}

	got, err := engine.Resolve(it, "amd64", manifest.Defaults{}, s)

	require.NoError(t, err)
	assert.Equal(t, []string{"cloudflared service install tok3n"}, got.PostInstall)
}

func TestResolveMissingSecretIsAnError(t *testing.T) {
	it := manifest.Item{
		ID: "cloudflared", CategoryID: "network", Type: manifest.ItemDeb,
		PostInstall: []string{"cloudflared service install {secret:CF_TUNNEL_TOKEN}"},
	}

	_, err := engine.Resolve(it, "amd64", manifest.Defaults{}, secrets.New())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "CF_TUNNEL_TOKEN")
}

func TestResolveUsesItemTimeoutOverDefault(t *testing.T) {
	it := manifest.Item{ID: "a", CategoryID: "c", Type: manifest.ItemApt, Timeout: 2 * time.Minute}

	got, err := engine.Resolve(it, "amd64", manifest.Defaults{Timeout: 15 * time.Minute}, secrets.New())

	require.NoError(t, err)
	assert.Equal(t, 2*time.Minute, got.Timeout)
}

func TestResolveFallsBackToDefaultTimeout(t *testing.T) {
	it := manifest.Item{ID: "a", CategoryID: "c", Type: manifest.ItemApt}

	got, err := engine.Resolve(it, "amd64", manifest.Defaults{Timeout: 15 * time.Minute}, secrets.New())

	require.NoError(t, err)
	assert.Equal(t, 15*time.Minute, got.Timeout)
}

func TestResolveFallsBackToBuiltInTimeout(t *testing.T) {
	it := manifest.Item{ID: "a", CategoryID: "c", Type: manifest.ItemApt}

	got, err := engine.Resolve(it, "amd64", manifest.Defaults{}, secrets.New())

	require.NoError(t, err)
	assert.Equal(t, engine.DefaultTimeout, got.Timeout)
}

func TestResolveNilStoreWithoutSecretsSucceeds(t *testing.T) {
	it := manifest.Item{
		ID: "go", Name: "Go", CategoryID: "dev", Type: manifest.ItemTarball,
		Version: "1.24.5",
		Source:  manifest.Source{"amd64": "https://go.dev/dl/go{version}.linux-amd64.tar.gz"},
	}

	got, err := engine.Resolve(it, "amd64", manifest.Defaults{}, nil)

	require.NoError(t, err, "a nil store must not panic for an item with no secrets")
	assert.Equal(t, "https://go.dev/dl/go1.24.5.linux-amd64.tar.gz", got.Source)
}

func TestResolveNilStoreWithSecretErrorsRatherThanPanicking(t *testing.T) {
	it := manifest.Item{
		ID: "cloudflared", Name: "Cloudflare Tunnel", CategoryID: "network", Type: manifest.ItemDeb,
		PostInstall: []string{"cloudflared service install {secret:CF_TUNNEL_TOKEN}"},
	}

	_, err := engine.Resolve(it, "amd64", manifest.Defaults{}, nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "CF_TUNNEL_TOKEN")
	assert.Contains(t, err.Error(), "network/cloudflared")
}

func TestResolvePopulatesComposeFields(t *testing.T) {
	it := manifest.Item{
		CategoryID: "containers", ID: "monitoring",
		Type:    manifest.ItemComposeStack,
		Path:    "stacks/monitoring",
		Files:   []string{"docker-compose.yaml", "prometheus.yml"},
		EnvFile: "optional",
		Secrets: []string{"CF_TUNNEL_TOKEN"},
	}

	got, err := engine.Resolve(it, "amd64", manifest.Defaults{}, nil)

	require.NoError(t, err)
	assert.Equal(t, "optional", got.EnvFile)
	assert.Equal(t, []string{"CF_TUNNEL_TOKEN"}, got.Secrets)
	assert.Equal(t, []string{"docker-compose.yaml", "prometheus.yml"}, got.Files)
}
