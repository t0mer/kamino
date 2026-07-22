package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/t0mer/kamino/internal/config"
)

func TestEnsureAPITokenGeneratesAndPersists(t *testing.T) {
	dir := t.TempDir()

	token, generated, err := ensureAPIToken(dir)

	require.NoError(t, err)
	assert.True(t, generated, "a fresh install must generate a token")
	assert.Len(t, token, 64)

	saved, err := config.Load(dir)
	require.NoError(t, err)
	assert.Equal(t, token, saved.APIToken)
}

func TestEnsureAPITokenReusesAnExistingOne(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, config.Save(dir, config.Settings{APIToken: "existing"}))

	token, generated, err := ensureAPIToken(dir)

	require.NoError(t, err)
	assert.False(t, generated, "an existing token must be reused, not replaced")
	assert.Equal(t, "existing", token)
}

func TestEnsureAPITokenPreservesRepoSettings(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, config.Save(dir, config.Settings{
		RepoURL: "https://github.com/t0mer/cfg", Ref: "main", RepoToken: "repo-s3cret",
	}))

	_, _, err := ensureAPIToken(dir)

	require.NoError(t, err)
	saved, err := config.Load(dir)
	require.NoError(t, err)
	assert.Equal(t, "https://github.com/t0mer/cfg", saved.RepoURL)
	assert.Equal(t, "repo-s3cret", saved.RepoToken,
		"generating an API token must not disturb the repo credential")
}

func TestIsLoopbackAddr(t *testing.T) {
	tests := []struct {
		addr string
		want bool
	}{
		{"127.0.0.1:8844", true},
		{"localhost:8844", true},
		{"[::1]:8844", true},
		{"0.0.0.0:8844", false},
		{"192.168.1.10:8844", false},
		{":8844", false},
	}
	for _, tc := range tests {
		t.Run(tc.addr, func(t *testing.T) {
			assert.Equal(t, tc.want, isLoopbackAddr(tc.addr))
		})
	}
}
