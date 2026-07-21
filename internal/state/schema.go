package state

// schemaSQL is applied on every Open. It is idempotent, so opening an existing
// database is safe.
const schemaSQL = `
PRAGMA journal_mode = WAL;
PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS runs (
    id          TEXT PRIMARY KEY,
    started_at  TIMESTAMP NOT NULL,
    finished_at TIMESTAMP,
    profile     TEXT NOT NULL DEFAULT '',
    config_sha  TEXT NOT NULL DEFAULT '',
    status      TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS steps (
    id          TEXT PRIMARY KEY,
    run_id      TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    seq         INTEGER NOT NULL,
    item_ref    TEXT NOT NULL,
    name        TEXT NOT NULL DEFAULT '',
    status      TEXT NOT NULL,
    started_at  TIMESTAMP,
    finished_at TIMESTAMP,
    exit_code   INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS logs (
    step_id TEXT NOT NULL REFERENCES steps(id) ON DELETE CASCADE,
    ts      TIMESTAMP NOT NULL,
    stream  TEXT NOT NULL,
    line    TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_steps_run ON steps(run_id, seq);
CREATE INDEX IF NOT EXISTS idx_logs_step ON logs(step_id);
`
