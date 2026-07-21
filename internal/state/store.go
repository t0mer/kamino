// Package state persists run history to sqlite.
package state

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	// Pure-Go sqlite driver: keeps CGO_ENABLED=0 so the binary cross-compiles
	// cleanly and runs on a scratch image.
	_ "modernc.org/sqlite"
)

// Status is the lifecycle state of a run or a step.
type Status string

// Run and step statuses.
const (
	StatusPending   Status = "pending"
	StatusRunning   Status = "running"
	StatusSuccess   Status = "success"
	StatusFailed    Status = "failed"
	StatusSkipped   Status = "skipped"
	StatusBlocked   Status = "blocked"
	StatusCancelled Status = "cancelled"
)

// Run is one execution of a plan.
type Run struct {
	ID         string     `json:"id"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	Profile    string     `json:"profile"`
	ConfigSHA  string     `json:"config_sha"`
	Status     Status     `json:"status"`
}

// Step is one item install within a run.
type Step struct {
	ID         string     `json:"id"`
	RunID      string     `json:"run_id"`
	ItemRef    string     `json:"item_ref"`
	Name       string     `json:"name"`
	Status     Status     `json:"status"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	ExitCode   int        `json:"exit_code"`
}

// Store is the sqlite-backed run history.
type Store struct {
	db *sql.DB
}

// Open opens (and if needed creates) the database at path.
func Open(path string) (*Store, error) {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("creating state dir: %w", err)
		}
	}

	db, err := sql.Open("sqlite", dsn(path))
	if err != nil {
		return nil, fmt.Errorf("opening state db: %w", err)
	}
	// modernc's driver serialises poorly under concurrent writers, and Kamino
	// only ever has one writer, so cap the pool rather than fight it. This is
	// purely a contention/performance choice: correctness of the ON DELETE
	// CASCADE that Prune relies on does NOT depend on it. foreign_keys is a
	// per-connection sqlite setting (off by default on every connection,
	// unlike journal_mode or the schema itself, which live in the database
	// file), so it is enabled via the "_pragma" DSN parameter in dsn() below
	// instead of a one-off PRAGMA exec. The driver (modernc.org/sqlite)
	// re-applies every "_pragma" DSN parameter each time it opens a new
	// physical connection, so foreign_keys stays on for any connection the
	// pool opens later too -- whether this cap is ever raised, or the sole
	// connection is discarded after an error and database/sql transparently
	// opens a replacement.
	db.SetMaxOpenConns(1)

	if _, err := db.Exec(schemaSQL); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("applying schema: %w", err)
	}
	return &Store{db: db}, nil
}

// dsn builds the sqlite connection string for path. It sets foreign_keys via
// the driver's "_pragma" query parameter (verified against
// modernc.org/sqlite v1.54.0: applyQueryParams runs for every connection
// newConn opens, not just the first) rather than a plain "PRAGMA foreign_keys
// = ON" exec, which only ever reaches whichever single connection happens to
// service that call.
func dsn(path string) string {
	return path + "?_pragma=foreign_keys(1)"
}

// Close releases the database.
func (s *Store) Close() error { return s.db.Close() }

// CreateRun inserts a new run.
func (s *Store) CreateRun(r Run) error {
	_, err := s.db.Exec(
		`INSERT INTO runs (id, started_at, profile, config_sha, status) VALUES (?, ?, ?, ?, ?)`,
		r.ID, r.StartedAt, r.Profile, r.ConfigSHA, string(r.Status))
	if err != nil {
		return fmt.Errorf("creating run: %w", err)
	}
	return nil
}

// FinishRun records a run's terminal status.
func (s *Store) FinishRun(id string, status Status, at time.Time) error {
	_, err := s.db.Exec(`UPDATE runs SET status = ?, finished_at = ? WHERE id = ?`,
		string(status), at, id)
	if err != nil {
		return fmt.Errorf("finishing run: %w", err)
	}
	return nil
}

// CreateStep inserts a step, preserving insertion order for later reads.
// seq is computed by a scalar subquery inside the INSERT itself, rather than
// a preceding "SELECT COUNT(*)" round-trip: the two-statement form is not
// atomic, so concurrent CreateStep calls for the same run can read the same
// count before either has inserted and collide on seq. A single INSERT
// statement (including its subquery) executes as one atomic unit of work
// under sqlite's writer lock, so every concurrent caller's subquery is
// guaranteed to observe the effects of every INSERT that already committed,
// for that run_id and any other run_id interleaved with it.
func (s *Store) CreateStep(st Step) error {
	_, err := s.db.Exec(
		`INSERT INTO steps (id, run_id, seq, item_ref, name, status, started_at, exit_code)
		 VALUES (?, ?, (SELECT COALESCE(MAX(seq), -1) + 1 FROM steps WHERE run_id = ?), ?, ?, ?, ?, ?)`,
		st.ID, st.RunID, st.RunID, st.ItemRef, st.Name, string(st.Status), st.StartedAt, st.ExitCode)
	if err != nil {
		return fmt.Errorf("creating step: %w", err)
	}
	return nil
}

// UpdateStepStatus moves a step to a new status. Terminal statuses also set
// finished_at; StatusRunning sets started_at.
func (s *Store) UpdateStepStatus(id string, status Status, at time.Time, exitCode int) error {
	query := `UPDATE steps SET status = ?, finished_at = ?, exit_code = ? WHERE id = ?`
	if status == StatusRunning {
		query = `UPDATE steps SET status = ?, started_at = ?, exit_code = ? WHERE id = ?`
	}
	if _, err := s.db.Exec(query, string(status), at, exitCode, id); err != nil {
		return fmt.Errorf("updating step status: %w", err)
	}
	return nil
}

// AppendLog stores one captured output line.
func (s *Store) AppendLog(stepID string, ts time.Time, stream, line string) error {
	_, err := s.db.Exec(`INSERT INTO logs (step_id, ts, stream, line) VALUES (?, ?, ?, ?)`,
		stepID, ts, stream, line)
	if err != nil {
		return fmt.Errorf("appending log: %w", err)
	}
	return nil
}

// ListRuns returns the most recent runs, newest first.
func (s *Store) ListRuns(limit int) ([]Run, error) {
	rows, err := s.db.Query(
		`SELECT id, started_at, finished_at, profile, config_sha, status
		 FROM runs ORDER BY started_at DESC, id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("listing runs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Run
	for rows.Next() {
		var r Run
		var status string
		if err := rows.Scan(&r.ID, &r.StartedAt, &r.FinishedAt, &r.Profile, &r.ConfigSHA, &status); err != nil {
			return nil, fmt.Errorf("scanning run: %w", err)
		}
		r.Status = Status(status)
		out = append(out, r)
	}
	return out, rows.Err()
}

// GetRun returns a run and its steps in execution order.
func (s *Store) GetRun(id string) (Run, []Step, error) {
	var r Run
	var status string
	err := s.db.QueryRow(
		`SELECT id, started_at, finished_at, profile, config_sha, status FROM runs WHERE id = ?`, id).
		Scan(&r.ID, &r.StartedAt, &r.FinishedAt, &r.Profile, &r.ConfigSHA, &status)
	if err != nil {
		return Run{}, nil, fmt.Errorf("loading run %q: %w", id, err)
	}
	r.Status = Status(status)

	rows, err := s.db.Query(
		`SELECT id, run_id, item_ref, name, status, started_at, finished_at, exit_code
		 FROM steps WHERE run_id = ? ORDER BY seq`, id)
	if err != nil {
		return Run{}, nil, fmt.Errorf("loading steps: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var steps []Step
	for rows.Next() {
		var st Step
		var stStatus string
		if err := rows.Scan(&st.ID, &st.RunID, &st.ItemRef, &st.Name, &stStatus,
			&st.StartedAt, &st.FinishedAt, &st.ExitCode); err != nil {
			return Run{}, nil, fmt.Errorf("scanning step: %w", err)
		}
		st.Status = Status(stStatus)
		steps = append(steps, st)
	}
	return r, steps, rows.Err()
}

// StepLogs returns a step's captured lines in order.
func (s *Store) StepLogs(stepID string) ([]string, error) {
	rows, err := s.db.Query(`SELECT line FROM logs WHERE step_id = ? ORDER BY ts, rowid`, stepID)
	if err != nil {
		return nil, fmt.Errorf("loading logs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			return nil, fmt.Errorf("scanning log line: %w", err)
		}
		out = append(out, line)
	}
	return out, rows.Err()
}

// Prune deletes all but the newest keep runs. Steps and logs go with them via
// ON DELETE CASCADE.
func (s *Store) Prune(keep int) error {
	_, err := s.db.Exec(
		`DELETE FROM runs WHERE id NOT IN (
		     SELECT id FROM runs ORDER BY started_at DESC, id DESC LIMIT ?
		 )`, keep)
	if err != nil {
		return fmt.Errorf("pruning runs: %w", err)
	}
	return nil
}
