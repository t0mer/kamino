package config_test

import (
	"encoding/hex"
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
		RepoURL:   "https://github.com/t0mer/cfg",
		Ref:       "main",
		RepoToken: "s3cret",
	}

	require.NoError(t, config.Save(dir, want))
	got, err := config.Load(dir)

	require.NoError(t, err)
	assert.Equal(t, want, got)
	assert.True(t, got.Configured())
}

func TestSaveUsesRestrictivePermissions(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, config.Save(dir, config.Settings{RepoURL: "https://x/y/z", RepoToken: "s3cret"}))

	info, err := os.Stat(filepath.Join(dir, "settings.json"))

	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(),
		"settings.json holds a token and must not be world-readable")
}

func TestResolvePrefersFlagsOverEnvOverSaved(t *testing.T) {
	t.Setenv("KAMINO_REPO", "https://github.com/env/repo")
	t.Setenv("KAMINO_REF", "env-ref")

	saved := config.Settings{RepoURL: "https://github.com/saved/repo", Ref: "saved-ref", RepoToken: "saved-token"}
	flags := config.Overrides{RepoURL: "https://github.com/flag/repo"}

	got := config.Resolve(saved, flags)

	assert.Equal(t, "https://github.com/flag/repo", got.RepoURL, "flag wins")
	assert.Equal(t, "env-ref", got.Ref, "env beats saved when no flag")
	assert.Equal(t, "saved-token", got.RepoToken, "saved is used when neither flag nor env is set")
}

func TestResolveDefaultsRefToMain(t *testing.T) {
	got := config.Resolve(config.Settings{RepoURL: "https://x/y/z"}, config.Overrides{})

	assert.Equal(t, "main", got.Ref)
}

func TestConfiguredRequiresRepoURL(t *testing.T) {
	assert.False(t, config.Settings{Ref: "main"}.Configured())
	assert.True(t, config.Settings{RepoURL: "https://x/y/z"}.Configured())
}

func TestSaveRemovesTempFileWhenRenameFails(t *testing.T) {
	dir := t.TempDir()
	// Pre-create settings.json as a directory so rename will fail
	settingsPath := filepath.Join(dir, "settings.json")
	require.NoError(t, os.Mkdir(settingsPath, 0o755))

	err := config.Save(dir, config.Settings{RepoURL: "https://example.com/repo", RepoToken: "s3cret"})

	// Save should return an error due to the failed rename
	require.Error(t, err)
	assert.Contains(t, err.Error(), "replacing settings")

	// The temp file should have been cleaned up
	tmpPath := filepath.Join(dir, "settings.json.tmp")
	_, statErr := os.Stat(tmpPath)
	assert.ErrorIs(t, statErr, os.ErrNotExist, "temp file should be removed after failed rename")
}

func TestSaveCreatesFreshDataDirWith0700Permissions(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "kamino-data")
	require.NoError(t, config.Save(dir, config.Settings{RepoURL: "https://example.com/repo"}))

	info, err := os.Stat(dir)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), info.Mode().Perm(),
		"data dir should be created with 0700 permissions for security")
}

func TestLoadIncludesFilePathInError(t *testing.T) {
	dir := t.TempDir()
	settingsPath := filepath.Join(dir, "settings.json")

	// Write invalid JSON to the file
	require.NoError(t, os.WriteFile(settingsPath, []byte("{invalid json"), 0o600))

	_, err := config.Load(dir)

	require.Error(t, err)
	assert.Contains(t, err.Error(), settingsPath,
		"error message should include the full path to the settings file for debugging")
}

func TestGenerateAPITokenIsRandomAndHex(t *testing.T) {
	a, err := config.GenerateAPIToken()
	require.NoError(t, err)
	b, err := config.GenerateAPIToken()
	require.NoError(t, err)

	assert.NotEqual(t, a, b, "each call must produce a fresh token")
	assert.Len(t, a, 64, "32 random bytes hex-encoded")
	_, err = hex.DecodeString(a)
	assert.NoError(t, err)
}

func TestAPITokenRoundTrips(t *testing.T) {
	dir := t.TempDir()
	want := config.Settings{RepoURL: "https://github.com/t0mer/cfg", APIToken: "abc123"}

	require.NoError(t, config.Save(dir, want))
	got, err := config.Load(dir)

	require.NoError(t, err)
	assert.Equal(t, "abc123", got.APIToken)
}

func TestExistingSettingsFileStillLoadsRepoToken(t *testing.T) {
	// A settings.json written before this change used the key "token" for the
	// repo token. Renaming the Go field must not orphan those files.
	dir := t.TempDir()
	legacy := `{"repo_url":"https://github.com/t0mer/cfg","ref":"main","token":"s3cret"}`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "settings.json"), []byte(legacy), 0o600))

	got, err := config.Load(dir)

	require.NoError(t, err)
	assert.Equal(t, "s3cret", got.RepoToken)
	assert.True(t, got.HasRepoToken())
}

func TestHasRepoTokenReportsPresence(t *testing.T) {
	assert.False(t, config.Settings{}.HasRepoToken())
	assert.True(t, config.Settings{RepoToken: "x"}.HasRepoToken())
}
