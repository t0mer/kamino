package manifest_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/t0mer/kamino/internal/manifest"
)

func resolvedFrom(t *testing.T, categoryFiles ...string) *manifest.Resolved {
	t.Helper()
	r := &manifest.Resolved{Manifest: manifest.Manifest{Schema: 1, Name: "t"}}
	for _, f := range categoryFiles {
		c, err := manifest.ParseCategory(read(t, f))
		require.NoError(t, err)
		r.Categories = append(r.Categories, *c)
	}
	return r
}

func TestValidateCleanFixturesHaveNoErrors(t *testing.T) {
	r := resolvedFrom(t,
		"config/categories/dev.yaml",
		"config/categories/tools.yaml",
		"config/categories/network.yaml",
	)
	problems := manifest.Validate(r)
	assert.Empty(t, problems.Errors(), "unexpected errors: %v", problems.Errors())
}

func TestValidateReportsMissingDependency(t *testing.T) {
	r := resolvedFrom(t, "invalid/missing-dep.yaml")
	errs := manifest.Validate(r).Errors()

	require.Len(t, errs, 1)
	assert.Equal(t, "dev/pypi-base", errs[0].Ref)
	assert.Contains(t, errs[0].Message, "dev/nonexistent")
}

func TestValidateReportsCycleOnce(t *testing.T) {
	r := resolvedFrom(t, "invalid/cycle.yaml")
	errs := manifest.Validate(r).Errors()

	require.Len(t, errs, 1)
	assert.Contains(t, errs[0].Message, "dependency cycle")
	assert.Contains(t, errs[0].Message, "dev/a")
	assert.Contains(t, errs[0].Message, "dev/b")
}

func TestValidateReportsMissingArchSource(t *testing.T) {
	r := resolvedFrom(t, "invalid/bad-arch.yaml")
	errs := manifest.Validate(r).Errors()

	require.Len(t, errs, 1)
	assert.Equal(t, "dev/go", errs[0].Ref)
	assert.Contains(t, errs[0].Message, "arm64")
}

func TestValidateRejectsPlainHTTP(t *testing.T) {
	r := resolvedFrom(t, "invalid/http-source.yaml")
	errs := manifest.Validate(r).Errors()

	require.NotEmpty(t, errs)
	assert.Contains(t, errs[0].Message, "https")
}

func TestValidateWarnsOnMissingChecksum(t *testing.T) {
	r := resolvedFrom(t, "config/categories/network.yaml")
	warns := manifest.Validate(r).Warnings()

	require.Len(t, warns, 1)
	assert.Equal(t, "network/cloudflared", warns[0].Ref)
	assert.Contains(t, warns[0].Message, "sha256")
}

func TestValidateAggregatesMultipleProblems(t *testing.T) {
	r := &manifest.Resolved{
		Manifest: manifest.Manifest{Schema: 1},
		Categories: []manifest.Category{{
			ID:   "dev",
			Name: "Dev",
			Items: []manifest.Item{
				{ID: "a", Name: "A", Type: "nonsense", CategoryID: "dev"},
				{ID: "b", Name: "B", Type: manifest.ItemApt, CategoryID: "dev"},
				{ID: "c", Name: "C", Type: manifest.ItemApt, Packages: []string{"c"},
					CheckContains: "x", CategoryID: "dev"},
			},
		}},
	}
	errs := manifest.Validate(r).Errors()

	// one unknown type, one apt without packages, one check_contains without check
	assert.Len(t, errs, 3)
}

func TestValidateRejectsWrongSchemaVersion(t *testing.T) {
	r := &manifest.Resolved{Manifest: manifest.Manifest{Schema: 2}}
	errs := manifest.Validate(r).Errors()

	require.Len(t, errs, 1)
	assert.Contains(t, errs[0].Message, "schema")
}
