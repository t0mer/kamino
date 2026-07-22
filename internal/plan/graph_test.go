package plan_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/t0mer/kamino/internal/manifest"
	"github.com/t0mer/kamino/internal/plan"
)

func refsOf(p *plan.Plan) []string {
	out := make([]string, 0, len(p.Steps))
	for _, s := range p.Steps {
		out = append(out, s.Ref)
	}
	return out
}

func TestBuildOrdersDependenciesFirst(t *testing.T) {
	r := loadFixture(t)

	got, err := plan.Build(r, profile(t, r, "production"), "amd64")

	require.NoError(t, err)
	refs := refsOf(got)

	dockerAt := indexOf(refs, "tools/docker")
	composeAt := indexOf(refs, "tools/docker-compose")
	require.NotEqual(t, -1, dockerAt)
	require.NotEqual(t, -1, composeAt)
	assert.Less(t, dockerAt, composeAt, "docker must be installed before the compose plugin")
}

func indexOf(refs []string, want string) int {
	for i, r := range refs {
		if r == want {
			return i
		}
	}
	return -1
}

func TestBuildIsDeterministic(t *testing.T) {
	r := loadFixture(t)

	first, err := plan.Build(r, profile(t, r, "dev"), "amd64")
	require.NoError(t, err)
	second, err := plan.Build(r, profile(t, r, "dev"), "amd64")
	require.NoError(t, err)

	assert.Equal(t, refsOf(first), refsOf(second))
}

func TestBuildTieBreaksByCategoryOrderThenFileOrder(t *testing.T) {
	r := loadFixture(t)

	got, err := plan.Build(r, profile(t, r, "dev"), "amd64")

	require.NoError(t, err)
	// dev has order 10, tools has order 20, so all dev items precede tools/jq.
	// Within dev, file order is go, python, pypi-base — and pypi-base depends
	// on python, which is already consistent with file order.
	assert.Equal(t, []string{"dev/go", "dev/python", "dev/pypi-base", "tools/jq"}, refsOf(got))
}

func TestBuildAppliesOverrides(t *testing.T) {
	r := loadFixture(t)

	got, err := plan.Build(r, profile(t, r, "production"), "amd64")

	require.NoError(t, err)
	for _, s := range got.Steps {
		if s.Ref == "tools/docker" {
			assert.Equal(t, "27.5", s.Version, "profile override must be applied")
			return
		}
	}
	t.Fatal("tools/docker not in plan")
}

func TestBuildMarksImplicitSteps(t *testing.T) {
	r := loadFixture(t)
	p := manifest.Profile{ID: "x", Include: []string{"tools/docker-compose"}}

	got, err := plan.Build(r, p, "amd64")

	require.NoError(t, err)
	for _, s := range got.Steps {
		if s.Ref == "tools/docker" {
			assert.True(t, s.Implicit)
			return
		}
	}
	t.Fatal("tools/docker not in plan")
}

func TestBuildCarriesConfigSHA(t *testing.T) {
	r := loadFixture(t)

	got, err := plan.Build(r, profile(t, r, "dev"), "amd64")

	require.NoError(t, err)
	assert.Equal(t, "abc123", got.ConfigSHA)
	assert.Equal(t, "amd64", got.Arch)
	assert.Equal(t, "dev", got.ProfileID)
}

func TestOrderRejectsCycles(t *testing.T) {
	index := map[string]manifest.Item{
		"c/a": {ID: "a", CategoryID: "c", DependsOn: []string{"c/b"}},
		"c/b": {ID: "b", CategoryID: "c", DependsOn: []string{"c/a"}},
	}
	rank := map[string]int{"c/a": 0, "c/b": 1}

	_, err := plan.Order([]string{"c/a", "c/b"}, index, rank)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "cycle")
}

func TestBuildWarnsOnMissingChecksum(t *testing.T) {
	r := loadFixture(t)

	got, err := plan.Build(r, manifest.Profile{ID: "x", Include: []string{"network/cloudflared"}}, "amd64")

	require.NoError(t, err)
	require.NotEmpty(t, got.Warnings)
	assert.Contains(t, got.Warnings[0], "sha256")
}

func TestBuildWarnsOnComposeStack(t *testing.T) {
	r := loadFixture(t)

	got, err := plan.Build(r, manifest.Profile{ID: "x", Include: []string{"tools/monitoring-stack"}}, "amd64")

	require.NoError(t, err)
	require.NotEmpty(t, got.Warnings)
	// Find the warning for the compose_stack (other deps may have warnings too)
	var found string
	for _, w := range got.Warnings {
		if strings.Contains(w, "tools/monitoring-stack") && strings.Contains(w, "docker compose") {
			found = w
			break
		}
	}
	require.NotEmpty(t, found, "expected warning for compose_stack not found in %v", got.Warnings)
}
