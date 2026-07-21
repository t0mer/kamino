package state

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPruneFailureIsRecoverable pins that a failing Prune surfaces as an error
// the caller can choose to ignore, rather than something that corrupts state.
//
// cmd/kamino deliberately logs and continues on a Prune error: trimming old
// history is housekeeping, and failing a provisioning run that actually
// succeeded — exiting non-zero to whatever is scripting it — would be a far
// worse outcome than keeping one extra run.
func TestPruneFailureIsRecoverable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "kamino.db")
	s, err := Open(path)
	require.NoError(t, err)

	now := time.Now().UTC()
	require.NoError(t, s.CreateRun(Run{ID: "run-1", Status: StatusSuccess, StartedAt: now}))

	// Closing the database makes the next statement fail the way a disk or
	// lock problem would at the end of an otherwise-successful run.
	require.NoError(t, s.Close())

	err = s.Prune(2)

	require.Error(t, err, "a broken prune must report, not panic")
	assert.Contains(t, err.Error(), "pruning runs")
}
