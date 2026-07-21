package plan_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/t0mer/kamino/internal/manifest"
	"github.com/t0mer/kamino/internal/plan"
)

func loadFixture(t *testing.T) *manifest.Resolved {
	t.Helper()
	root := filepath.Join("..", "..", "testdata", "config")

	mb, err := os.ReadFile(filepath.Join(root, "manifest.yaml"))
	require.NoError(t, err)
	m, err := manifest.ParseManifest(mb)
	require.NoError(t, err)

	out := &manifest.Resolved{Manifest: *m, SHA: "abc123"}
	for _, p := range m.Categories {
		b, err := os.ReadFile(filepath.Join(root, p))
		require.NoError(t, err)
		c, err := manifest.ParseCategory(b)
		require.NoError(t, err)
		out.Categories = append(out.Categories, *c)
	}
	for _, p := range m.Profiles {
		b, err := os.ReadFile(filepath.Join(root, p))
		require.NoError(t, err)
		pr, err := manifest.ParseProfile(b)
		require.NoError(t, err)
		out.Profiles = append(out.Profiles, *pr)
	}
	return out
}

func profile(t *testing.T, r *manifest.Resolved, id string) manifest.Profile {
	t.Helper()
	for _, p := range r.Profiles {
		if p.ID == id {
			return p
		}
	}
	t.Fatalf("profile %q not found", id)
	return manifest.Profile{}
}

func TestSelectExpandsGlobs(t *testing.T) {
	r := loadFixture(t)

	got, err := plan.Select(r, profile(t, r, "dev"), "amd64")

	require.NoError(t, err)
	assert.ElementsMatch(t,
		[]string{"dev/go", "dev/python", "dev/pypi-base", "tools/jq"},
		got.Refs)
}

func TestSelectAppliesExclude(t *testing.T) {
	r := loadFixture(t)

	got, err := plan.Select(r, profile(t, r, "production"), "amd64")

	require.NoError(t, err)
	for _, ref := range got.Refs {
		assert.NotContains(t, ref, "dev/", "dev/* was excluded")
	}
	assert.Contains(t, got.Refs, "tools/docker")
	assert.Contains(t, got.Refs, "network/cloudflared")
}

func TestSelectAutoAddsMissingDependency(t *testing.T) {
	r := loadFixture(t)
	p := manifest.Profile{ID: "x", Include: []string{"tools/docker-compose"}}

	got, err := plan.Select(r, p, "amd64")

	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"tools/docker", "tools/docker-compose"}, got.Refs)
	assert.True(t, got.Implicit["tools/docker"], "auto-added dependency must be marked implicit")
	assert.False(t, got.Implicit["tools/docker-compose"], "explicitly included item is not implicit")
}

func TestSelectAddsDependenciesTransitively(t *testing.T) {
	r := &manifest.Resolved{Categories: []manifest.Category{{
		ID: "c", Name: "C",
		Items: []manifest.Item{
			{ID: "a", Name: "A", Type: manifest.ItemApt, CategoryID: "c", DependsOn: []string{"c/b"}},
			{ID: "b", Name: "B", Type: manifest.ItemApt, CategoryID: "c", DependsOn: []string{"c/d"}},
			{ID: "d", Name: "D", Type: manifest.ItemApt, CategoryID: "c"},
		},
	}}}

	got, err := plan.Select(r, manifest.Profile{ID: "x", Include: []string{"c/a"}}, "amd64")

	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"c/a", "c/b", "c/d"}, got.Refs)
	assert.True(t, got.Implicit["c/d"])
}

func TestSelectExplicitlyExcludedDependencyIsAnError(t *testing.T) {
	r := loadFixture(t)
	p := manifest.Profile{
		ID:      "x",
		Include: []string{"tools/docker-compose"},
		Exclude: []string{"tools/docker"},
	}

	_, err := plan.Select(r, p, "amd64")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "tools/docker")
	assert.Contains(t, err.Error(), "excluded")
}

func TestSelectSkipsItemsForOtherArch(t *testing.T) {
	r := &manifest.Resolved{Categories: []manifest.Category{{
		ID: "c", Name: "C",
		Items: []manifest.Item{
			{ID: "a", Name: "A", Type: manifest.ItemApt, CategoryID: "c", Arch: []string{"arm64"}},
			{ID: "b", Name: "B", Type: manifest.ItemApt, CategoryID: "c"},
		},
	}}}

	got, err := plan.Select(r, manifest.Profile{ID: "x", Include: []string{"c/*"}}, "amd64")

	require.NoError(t, err)
	assert.Equal(t, []string{"c/b"}, got.Refs)
}

func TestSelectDependencyForOtherArchIsAnError(t *testing.T) {
	r := &manifest.Resolved{Categories: []manifest.Category{{
		ID: "c", Name: "C",
		Items: []manifest.Item{
			{ID: "a", Name: "A", Type: manifest.ItemApt, CategoryID: "c", Arch: []string{"amd64"}, DependsOn: []string{"c/b"}},
			{ID: "b", Name: "B", Type: manifest.ItemApt, CategoryID: "c", Arch: []string{"arm64"}},
		},
	}}}

	_, err := plan.Select(r, manifest.Profile{ID: "x", Include: []string{"c/a"}}, "amd64")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "c/a")
	assert.Contains(t, err.Error(), "c/b")
	assert.Contains(t, err.Error(), "amd64")
}

func TestSelectTransitiveDependencyForOtherArchIsAnError(t *testing.T) {
	r := &manifest.Resolved{Categories: []manifest.Category{{
		ID: "c", Name: "C",
		Items: []manifest.Item{
			{ID: "a", Name: "A", Type: manifest.ItemApt, CategoryID: "c", DependsOn: []string{"c/b"}},
			{ID: "b", Name: "B", Type: manifest.ItemApt, CategoryID: "c", DependsOn: []string{"c/d"}},
			{ID: "d", Name: "D", Type: manifest.ItemApt, CategoryID: "c", Arch: []string{"arm64"}},
		},
	}}}

	_, err := plan.Select(r, manifest.Profile{ID: "x", Include: []string{"c/a"}}, "amd64")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "c/b")
	assert.Contains(t, err.Error(), "c/d")
	assert.Contains(t, err.Error(), "amd64")
}

func TestSelectAddsArchCompatibleDependency(t *testing.T) {
	r := &manifest.Resolved{Categories: []manifest.Category{{
		ID: "c", Name: "C",
		Items: []manifest.Item{
			{ID: "a", Name: "A", Type: manifest.ItemApt, CategoryID: "c", DependsOn: []string{"c/b"}},
			{ID: "b", Name: "B", Type: manifest.ItemApt, CategoryID: "c", Arch: []string{"amd64"}},
		},
	}}}

	got, err := plan.Select(r, manifest.Profile{ID: "x", Include: []string{"c/a"}}, "amd64")

	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"c/a", "c/b"}, got.Refs)
	assert.True(t, got.Implicit["c/b"], "arch-compatible auto-added dependency must be marked implicit")
}

func TestSelectUnknownIncludeRefIsAnError(t *testing.T) {
	r := loadFixture(t)

	_, err := plan.Select(r, manifest.Profile{ID: "x", Include: []string{"dev/nope"}}, "amd64")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "dev/nope")
}
