package main

import (
	"errors"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRunInTempDirRemovesDirAfterUse proves the gap flagged in review is
// closed: runners materialise downloaded artefacts and fetched scripts into
// the run temp dir, and nothing else on the apply path cleans them up. A
// root-executable script left behind after a run matters.
func TestRunInTempDirRemovesDirAfterUse(t *testing.T) {
	var seen string
	path, err := runInTempDir(func(tempDir string) error {
		seen = tempDir
		_, statErr := os.Stat(tempDir)
		require.NoError(t, statErr, "temp dir must exist while fn runs")
		return nil
	})

	require.NoError(t, err)
	assert.Equal(t, seen, path)

	_, statErr := os.Stat(path)
	assert.True(t, os.IsNotExist(statErr), "temp dir must not survive the run")
}

// TestRunInTempDirRemovesDirEvenOnError proves cleanup still happens when the
// run fails or is cancelled mid-way — the defer must fire on every return
// path, not just the success path.
func TestRunInTempDirRemovesDirEvenOnError(t *testing.T) {
	path, err := runInTempDir(func(tempDir string) error {
		return errors.New("boom")
	})

	require.Error(t, err)
	require.NotEmpty(t, path)

	_, statErr := os.Stat(path)
	assert.True(t, os.IsNotExist(statErr), "temp dir must be cleaned up even when fn fails")
}
