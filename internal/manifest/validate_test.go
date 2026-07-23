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

func TestValidateReportsDuplicateCategoryID(t *testing.T) {
	r := &manifest.Resolved{
		Manifest: manifest.Manifest{Schema: 1},
		Categories: []manifest.Category{
			{ID: "dev", Name: "Dev"},
			{ID: "dev", Name: "Dev Again"},
		},
	}
	errs := manifest.Validate(r).Errors()

	require.Len(t, errs, 1)
	assert.Equal(t, "categories/dev.yaml", errs[0].File)
	assert.Equal(t, "id", errs[0].Field)
	assert.Equal(t, `duplicate category id "dev"`, errs[0].Message)
}

func TestValidateReportsDuplicateItemID(t *testing.T) {
	r := &manifest.Resolved{
		Manifest: manifest.Manifest{Schema: 1},
		Categories: []manifest.Category{{
			ID:   "dev",
			Name: "Dev",
			Items: []manifest.Item{
				{ID: "go", Name: "Go", Type: manifest.ItemApt, Packages: []string{"go"}, CategoryID: "dev"},
				{ID: "go", Name: "Go Again", Type: manifest.ItemApt, Packages: []string{"go"}, CategoryID: "dev"},
			},
		}},
	}
	errs := manifest.Validate(r).Errors()

	require.Len(t, errs, 1)
	assert.Equal(t, "categories/dev.yaml", errs[0].File)
	assert.Equal(t, "dev/go", errs[0].Ref)
	assert.Equal(t, "id", errs[0].Field)
	assert.Equal(t, "duplicate item id", errs[0].Message)
}

func TestValidateOrdersSourceProblemsDeterministically(t *testing.T) {
	r := &manifest.Resolved{
		Manifest: manifest.Manifest{Schema: 1},
		Categories: []manifest.Category{{
			ID:   "dev",
			Name: "Dev",
			Items: []manifest.Item{{
				ID:      "bad",
				Name:    "Bad",
				Type:    manifest.ItemTarball,
				Version: "1.0.0",
				Source: manifest.Source{
					"amd64": "http://example.com/bad-amd64.tar.gz",
					"arm64": "http://example.com/bad-arm64.tar.gz",
				},
				SHA256: manifest.Source{
					"amd64": "deadbeef",
					"arm64": "deadbeef",
				},
				CategoryID: "dev",
			}},
		}},
	}
	errs := manifest.Validate(r).Errors()

	require.Len(t, errs, 2)
	assert.Contains(t, errs[0].Message, "source for arch amd64 must use https")
	assert.Contains(t, errs[1].Message, "source for arch arm64 must use https")
}

// errorsForScriptSource validates a single script item whose source is src and
// returns the resulting errors. src is a bare string, which the parser would
// normally fan out across arches; here it is set directly on both.
func errorsForScriptSource(t *testing.T, src string) manifest.Problems {
	t.Helper()
	r := &manifest.Resolved{
		Manifest: manifest.Manifest{Schema: 1},
		Categories: []manifest.Category{{
			ID:   "tools",
			Name: "Tools",
			Items: []manifest.Item{{
				ID:         "s",
				Name:       "S",
				Type:       manifest.ItemScript,
				Source:     manifest.Source{"amd64": src, "arm64": src},
				CategoryID: "tools",
			}},
		}},
	}
	return manifest.Validate(r).Errors()
}

func TestValidateAllowsLocalScriptSource(t *testing.T) {
	assert.Empty(t, errorsForScriptSource(t, "scripts/setup.sh"),
		"a repo-relative script path must be accepted")
	assert.Empty(t, errorsForScriptSource(t, "https://get.docker.com"),
		"an https script source must still be accepted")
}

func TestValidateRejectsUnsafeAndInsecureScriptSource(t *testing.T) {
	// A non-https URL scheme is not a local path; it must be rejected.
	assert.NotEmpty(t, errorsForScriptSource(t, "http://evil.example/x.sh"),
		"an http script source must be rejected")
	// Absolute paths and parent traversal escape the config repo root.
	assert.NotEmpty(t, errorsForScriptSource(t, "/etc/passwd"),
		"an absolute script source must be rejected")
	assert.NotEmpty(t, errorsForScriptSource(t, "../../etc/passwd"),
		"a traversing script source must be rejected")
}

func TestValidateStillRequiresHTTPSForNonScriptLocalSource(t *testing.T) {
	// The local-path allowance is script-only: a tarball with a bare path must
	// still be rejected as non-https.
	r := &manifest.Resolved{
		Manifest: manifest.Manifest{Schema: 1},
		Categories: []manifest.Category{{
			ID: "dev", Name: "Dev",
			Items: []manifest.Item{{
				ID: "t", Name: "T", Type: manifest.ItemTarball, Version: "1.0.0",
				Source:     manifest.Source{"amd64": "scripts/x.tar.gz", "arm64": "scripts/x.tar.gz"},
				CategoryID: "dev",
			}},
		}},
	}
	errs := manifest.Validate(r).Errors()
	require.NotEmpty(t, errs)
	assert.Contains(t, errs[0].Message, "https")
}

func TestValidateComposeStackRequiresPathFilesAndComposeFile(t *testing.T) {
	mk := func(it manifest.Item) manifest.Problems {
		it.CategoryID = "containers"
		r := &manifest.Resolved{
			Manifest:   manifest.Manifest{Schema: 1},
			Categories: []manifest.Category{{ID: "containers", Items: []manifest.Item{it}}},
		}
		return manifest.Validate(r).Errors()
	}

	noPath := mk(manifest.Item{ID: "a", Type: manifest.ItemComposeStack, Files: []string{"docker-compose.yaml"}})
	assert.NotEmpty(t, noPath, "missing path must be an error")

	noFiles := mk(manifest.Item{ID: "b", Type: manifest.ItemComposeStack, Path: "stacks/x"})
	assert.NotEmpty(t, noFiles, "missing files must be an error")

	noCompose := mk(manifest.Item{ID: "c", Type: manifest.ItemComposeStack, Path: "stacks/x", Files: []string{"prometheus.yml"}})
	assert.NotEmpty(t, noCompose, "files without a compose file must be an error")

	ok := mk(manifest.Item{ID: "d", Type: manifest.ItemComposeStack, Path: "stacks/x", Files: []string{"docker-compose.yaml"}})
	assert.Empty(t, ok, "a well-formed compose_stack has no errors")
}
