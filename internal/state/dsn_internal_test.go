package state

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDSNSurvivesAwkwardPaths pins that a data dir containing DSN-significant
// bytes still opens the intended file with foreign_keys enabled.
//
// The driver splits a DSN on its first "?", so concatenating the path onto a
// query string silently truncated the filename AND dropped the pragma —
// leaving Prune's ON DELETE CASCADE inoperative with nothing failing. Paths
// come from --data-dir, so they are user-supplied.
func TestDSNSurvivesAwkwardPaths(t *testing.T) {
	names := map[string]string{
		"question mark": "has?question.db",
		"ampersand":     "has&ersand.db",
		"hash":          "has#hash.db",
		"space":         "has space.db",
		"percent":       "has%percent.db",
		"equals":        "has=equals.db",
	}

	for name, file := range names {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), file)

			s, err := Open(path)
			require.NoError(t, err)
			t.Cleanup(func() { _ = s.Close() })

			// The intended file must exist — not a truncated prefix of it.
			assert.FileExists(t, path)

			// Open a second, independent handle on the same DSN: the pragma
			// must hold on a connection that never ran the schema.
			db, err := sql.Open("sqlite", dsn(path))
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })

			var fk int
			require.NoError(t, db.QueryRow("PRAGMA foreign_keys").Scan(&fk))
			assert.Equal(t, 1, fk, "foreign_keys must be on, or Prune stops cascading")
		})
	}
}
