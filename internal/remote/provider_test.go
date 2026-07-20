package remote_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/t0mer/kamino/internal/remote"
)

func TestParseRepoDetectsProviders(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		provider remote.Provider
		owner    string
		repo     string
	}{
		{"github https", "https://github.com/t0mer/kamino-config", remote.ProviderGitHub, "t0mer", "kamino-config"},
		{"github with .git", "https://github.com/t0mer/kamino-config.git", remote.ProviderGitHub, "t0mer", "kamino-config"},
		{"github trailing slash", "https://github.com/t0mer/kamino-config/", remote.ProviderGitHub, "t0mer", "kamino-config"},
		{"gitlab", "https://gitlab.com/t0mer/kamino-config", remote.ProviderGitLab, "t0mer", "kamino-config"},
		{"gitea host", "https://git.example.com/t0mer/kamino-config", remote.ProviderGeneric, "t0mer", "kamino-config"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, err := remote.ParseRepo(tc.url, "main", "")
			require.NoError(t, err)
			assert.Equal(t, tc.provider, r.Provider)
			assert.Equal(t, tc.owner, r.Owner)
			assert.Equal(t, tc.repo, r.Name)
		})
	}
}

func TestRawURLPerProvider(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want string
	}{
		{
			"github",
			"https://github.com/t0mer/kamino-config",
			"https://raw.githubusercontent.com/t0mer/kamino-config/abc123/manifest.yaml",
		},
		{
			"gitlab",
			"https://gitlab.com/t0mer/kamino-config",
			"https://gitlab.com/t0mer/kamino-config/-/raw/abc123/manifest.yaml",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, err := remote.ParseRepo(tc.url, "main", "")
			require.NoError(t, err)
			assert.Equal(t, tc.want, r.RawURL("abc123", "manifest.yaml"))
		})
	}
}

func TestRawBaseTemplateOverridesProvider(t *testing.T) {
	r, err := remote.ParseRepo(
		"https://git.example.com/t0mer/cfg", "main",
		"https://git.example.com/t0mer/cfg/raw/branch/{ref}/{path}",
	)
	require.NoError(t, err)

	assert.Equal(t,
		"https://git.example.com/t0mer/cfg/raw/branch/abc123/categories/dev.yaml",
		r.RawURL("abc123", "categories/dev.yaml"))
}

func TestGenericProviderWithoutTemplateIsError(t *testing.T) {
	_, err := remote.ParseRepo("https://git.example.com/t0mer/cfg", "main", "")
	require.NoError(t, err, "parsing succeeds; the error surfaces when building a raw URL")

	r, _ := remote.ParseRepo("https://git.example.com/t0mer/cfg", "main", "")
	assert.Equal(t, "", r.RawURL("abc123", "manifest.yaml"),
		"a generic host with no template cannot build a raw URL")
}

func TestParseRepoRejectsNonHTTPS(t *testing.T) {
	_, err := remote.ParseRepo("http://github.com/t0mer/cfg", "main", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "https")
}

func TestParseRepoRejectsMalformedPath(t *testing.T) {
	_, err := remote.ParseRepo("https://github.com/t0mer", "main", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "owner/repo")
}

func TestParseRepoDefaultsRefToMain(t *testing.T) {
	r, err := remote.ParseRepo("https://github.com/t0mer/cfg", "", "")
	require.NoError(t, err)
	assert.Equal(t, "main", r.Ref)
}

func TestAuthHeaderPerProvider(t *testing.T) {
	tests := []struct {
		url       string
		wantName  string
		wantValue string
	}{
		{"https://github.com/t0mer/cfg", "Authorization", "token s3cret"},
		{"https://gitlab.com/t0mer/cfg", "PRIVATE-TOKEN", "s3cret"},
	}
	for _, tc := range tests {
		r, err := remote.ParseRepo(tc.url, "main", "")
		require.NoError(t, err)
		r.Token = "s3cret"

		name, value, ok := r.AuthHeader()
		require.True(t, ok)
		assert.Equal(t, tc.wantName, name)
		assert.Equal(t, tc.wantValue, value)
	}
}

func TestAuthHeaderAbsentWithoutToken(t *testing.T) {
	r, err := remote.ParseRepo("https://github.com/t0mer/cfg", "main", "")
	require.NoError(t, err)

	_, _, ok := r.AuthHeader()
	assert.False(t, ok)
}
