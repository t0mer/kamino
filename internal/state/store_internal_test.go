package state

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCreateStepSeqIsUniqueUnderConcurrency proves CreateStep's seq
// assignment is atomic. It spawns many goroutines calling CreateStep for the
// same run concurrently, then reads the raw seq column back (GetRun does not
// expose it) and asserts every value is distinct and gap-free -- i.e. the
// set {0, ..., n-1} -- which is only possible if each INSERT's seq
// computation saw the effects of every previously committed INSERT. The old
// "SELECT COUNT(*) then INSERT" implementation raced across its two
// round-trips and produced duplicate seq values under this same test (see
// the revert-proof in the task report).
func TestCreateStepSeqIsUniqueUnderConcurrency(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "kamino.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	now := time.Now().UTC()
	require.NoError(t, s.CreateRun(Run{ID: "run-1", Status: StatusRunning, StartedAt: now}))
	require.NoError(t, s.CreateRun(Run{ID: "run-2", Status: StatusRunning, StartedAt: now}))

	const n = 20
	var wg sync.WaitGroup
	errs := make([]error, 2*n)
	// Interleave concurrent CreateStep calls across two different runs, per
	// the fix's contract: atomic within a run_id AND unaffected by
	// concurrent activity on other run_ids.
	for i := 0; i < n; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			errs[i] = s.CreateStep(Step{
				ID: fmt.Sprintf("run1-step-%d", i), RunID: "run-1",
				ItemRef: fmt.Sprintf("c/item-%d", i), Status: StatusPending,
			})
		}(i)
		go func(i int) {
			defer wg.Done()
			errs[n+i] = s.CreateStep(Step{
				ID: fmt.Sprintf("run2-step-%d", i), RunID: "run-2",
				ItemRef: fmt.Sprintf("c/item-%d", i), Status: StatusPending,
			})
		}(i)
	}
	wg.Wait()
	for _, err := range errs {
		require.NoError(t, err)
	}

	assertSeqsAreDistinctAndGapFree(t, s.db, "run-1", n)
	assertSeqsAreDistinctAndGapFree(t, s.db, "run-2", n)

	// GetRun must return them in that same stable, execution (seq) order --
	// stable across repeated reads, since seq is fixed at insert time.
	_, steps1, err := s.GetRun("run-1")
	require.NoError(t, err)
	require.Len(t, steps1, n)
	_, steps1Again, err := s.GetRun("run-1")
	require.NoError(t, err)
	assert.Equal(t, refs(steps1), refs(steps1Again), "GetRun order must be stable across repeated reads")
}

func assertSeqsAreDistinctAndGapFree(t *testing.T, db *sql.DB, runID string, n int) {
	t.Helper()
	rows, err := db.QueryContext(context.Background(),
		`SELECT seq FROM steps WHERE run_id = ? ORDER BY seq`, runID)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()

	seqs := make([]int, 0, n)
	for rows.Next() {
		var seq int
		require.NoError(t, rows.Scan(&seq))
		seqs = append(seqs, seq)
	}
	require.NoError(t, rows.Err())
	require.Len(t, seqs, n, "run %q: expected %d steps", runID, n)

	seen := make(map[int]bool, n)
	for _, seq := range seqs {
		assert.Falsef(t, seen[seq], "run %q: duplicate seq %d assigned to two steps", runID, seq)
		seen[seq] = true
	}
	for want := 0; want < n; want++ {
		assert.Truef(t, seen[want], "run %q: seq %d missing (gap)", runID, want)
	}
}

func refs(steps []Step) []string {
	out := make([]string, len(steps))
	for i, st := range steps {
		out[i] = st.ItemRef
	}
	return out
}

// TestForeignKeysPragmaAppliesBeyondFirstConnection proves that
// foreign_keys enforcement -- which Prune's ON DELETE CASCADE depends on --
// does not depend on the pool being capped at one connection. It opens a
// pool against the same dsn() Open uses, raises the cap, and forces several
// distinct physical connections open concurrently (via db.Conn, which pins
// each one until released), then asserts PRAGMA foreign_keys reads 1 on
// every single one of them -- including connections beyond the first, which
// a one-off "PRAGMA foreign_keys = ON" exec (the pre-fix approach) would
// never reach.
func TestForeignKeysPragmaAppliesBeyondFirstConnection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "kamino.db")
	db, err := sql.Open("sqlite", dsn(path))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	const conns = 5
	db.SetMaxOpenConns(conns)

	ctx := context.Background()
	pinned := make([]*sql.Conn, conns)
	for i := 0; i < conns; i++ {
		c, err := db.Conn(ctx)
		require.NoError(t, err)
		pinned[i] = c
		t.Cleanup(func() { _ = c.Close() })
	}

	require.Equal(t, conns, db.Stats().OpenConnections, "test setup must actually force multiple physical connections")

	for i, c := range pinned {
		var fk int
		require.NoError(t, c.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&fk))
		assert.Equalf(t, 1, fk, "connection %d: foreign_keys must be ON", i)
	}
}
