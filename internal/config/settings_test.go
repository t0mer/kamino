package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/t0mer/kamino/internal/config"
)

func TestLoadMissingFileIsNotAnError(t *testing.T) {
	s, err := config.Load(t.TempDir())

	require.NoError(t, err)
	assert.False(t, s.Configured())
}

func TestSaveThenLoadRoundTrips(t *testing.T) {
	dir := t.TempDir()
	want := config.Settings{
		RepoURL: "https://github.com/t0mer/cfg",
		Ref:     "main",
		Token:   "s3cret",
	}

	require.NoError(t, config.Save(dir, want))
	got, err := config.Load(dir)

	require.NoError(t, err)
	assert.Equal(t, want, got)
	assert.True(t, got.Configured())
}

func TestSaveUsesRestrictivePermissions(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, config.Save(dir, config.Settings{RepoURL: "https://x/y/z", Token: "s3cret"}))

	info, err := os.Stat(filepath.Join(dir, "settings.json"))

	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(),
		"settings.json holds a token and must not be world-readable")
}

func TestResolvePrefersFlagsOverEnvOverSaved(t *testing.T) {
	t.Setenv("KAMINO_REPO", "https://github.com/env/repo")
	t.Setenv("KAMINO_REF", "env-ref")

	saved := config.Settings{RepoURL: "https://github.com/saved/repo", Ref: "saved-ref", Token: "saved-token"}
	flags := config.Overrides{RepoURL: "https://github.com/flag/repo"}

	got := config.Resolve(saved, flags)

	assert.Equal(t, "https://github.com/flag/repo", got.RepoURL, "flag wins")
	assert.Equal(t, "env-ref", got.Ref, "env beats saved when no flag")
	assert.Equal(t, "saved-token", got.Token, "saved is used when neither flag nor env is set")
}

func TestResolveDefaultsRefToMain(t *testing.T) {
	got := config.Resolve(config.Settings{RepoURL: "https://x/y/z"}, config.Overrides{})

	assert.Equal(t, "main", got.Ref)
}

func TestConfiguredRequiresRepoURL(t *testing.T) {
	assert.False(t, config.Settings{Ref: "main"}.Configured())
	assert.True(t, config.Settings{RepoURL: "https://x/y/z"}.Configured())
}
