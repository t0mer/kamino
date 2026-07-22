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

func TestGenericProviderWithoutTemplateRawURLIsEmpty(t *testing.T) {
	r, err := remote.ParseRepo("https://git.example.com/t0mer/cfg", "main", "")
	require.NoError(t, err)

	assert.Equal(t, "", r.RawURL("abc123", "manifest.yaml"),
		"a generic host with no template cannot build a raw URL")
}

func TestParseRepoRejectsRawBaseTemplateMissingRef(t *testing.T) {
	_, err := remote.ParseRepo("https://git.example.com/t0mer/cfg", "main", "https://git.example.com/t0mer/cfg/{path}")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "{ref}")
}

func TestParseRepoRejectsRawBaseTemplateMissingPath(t *testing.T) {
	_, err := remote.ParseRepo("https://git.example.com/t0mer/cfg", "main", "https://git.example.com/t0mer/cfg/{ref}")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "{path}")
}

func TestParseRepoAcceptsRawBaseTemplateWithBothPlaceholders(t *testing.T) {
	r, err := remote.ParseRepo(
		"https://git.example.com/t0mer/cfg", "main",
		"https://git.example.com/t0mer/cfg/raw/branch/{ref}/{path}",
	)
	require.NoError(t, err)
	assert.Equal(t, "https://git.example.com/t0mer/cfg/raw/branch/{ref}/{path}", r.RawBaseTemplate)
}

func TestParseRepoDetectsProviderCaseInsensitiveHost(t *testing.T) {
	r, err := remote.ParseRepo("https://GitHub.COM/t0mer/kamino-config", "main", "")
	require.NoError(t, err)
	assert.Equal(t, remote.ProviderGitHub, r.Provider)
}

func TestParseRepoDetectsProviderWithPort(t *testing.T) {
	r, err := remote.ParseRepo("https://github.com:8443/t0mer/kamino-config", "main", "")
	require.NoError(t, err)
	assert.Equal(t, remote.ProviderGitHub, r.Provider)
}

func TestRawURLSelfHostedGitLabWithPort(t *testing.T) {
	// gitlab.com on a non-standard port still detects as ProviderGitLab
	// (host matching ignores the port), and the port must be preserved in
	// r.Host so the built raw URL still points at it.
	r, err := remote.ParseRepo("https://gitlab.com:8443/t0mer/kamino-config", "main", "")
	require.NoError(t, err)
	require.Equal(t, remote.ProviderGitLab, r.Provider)

	assert.Equal(t,
		"https://gitlab.com:8443/t0mer/kamino-config/-/raw/abc123/manifest.yaml",
		r.RawURL("abc123", "manifest.yaml"))
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

func TestParseRepoDoesNotEchoACredentialInAParseError(t *testing.T) {
	// A control character makes url.Parse fail. The URL carries a token in its
	// userinfo, which must not surface in the error — it reaches an HTTP
	// response and the logs behind it.
	_, err := remote.ParseRepo("https://user:sup3rs3cret@host\x7f/o/r", "main", "")

	require.Error(t, err)
	assert.NotContains(t, err.Error(), "sup3rs3cret",
		"a credential embedded in the URL must not be echoed in the error")
}
