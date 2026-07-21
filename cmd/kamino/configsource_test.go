package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestConfigSourceFailsClosedOnUnresolvableSettings proves a fix for a
// review finding: when --config-dir is unset and settings.json cannot be
// turned into a usable repo (here, a corrupt settings file), configSource
// must return a source that errors on every Fetch, never a dirFetcher
// rooted at "" — which would silently resolve a repo-relative script path
// against the process's current working directory instead of the pinned
// config repo, and execute whatever unrelated file happened to be there, as
// root.
func TestConfigSourceFailsClosedOnUnresolvableSettings(t *testing.T) {
	dataDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dataDir, "settings.json"), []byte("not json"), 0o600))

	saved := flags
	flags = globalFlags{dataDir: dataDir}
	defer func() { flags = saved }()

	src := configSource(context.Background())

	_, isDirFetcher := src.(dirFetcher)
	assert.False(t, isDirFetcher, "must not fall back to a local-directory fetcher on settings error")

	_, err := src.Fetch(context.Background(), "main", "scripts/anything.sh")
	assert.Error(t, err, "a source returned after an unresolvable settings error must fail every Fetch")
}
