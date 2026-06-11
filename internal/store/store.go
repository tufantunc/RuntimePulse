// Package store persists RuntimePulse state in a single SQLite database.
package store

import (
	"database/sql"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite",
		"file:"+path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	// modernc/sqlite allows one writer; a single connection keeps the
	// outbox transaction serialization trivial.
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

const schema = `
CREATE TABLE IF NOT EXISTS events (
  id         TEXT PRIMARY KEY,
  type       TEXT NOT NULL,
  source     TEXT NOT NULL,
  payload    TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_events_type_source ON events(type, source);
CREATE INDEX IF NOT EXISTS idx_events_created ON events(created_at);

CREATE TABLE IF NOT EXISTS rules (
  id              TEXT PRIMARY KEY,
  event_type      TEXT NOT NULL,
  event_source    TEXT NOT NULL DEFAULT '',
  action_kind     TEXT NOT NULL,
  session_id      TEXT NOT NULL,
  prompt_template TEXT NOT NULL,
  label           TEXT NOT NULL DEFAULT '',
  one_shot        INTEGER NOT NULL DEFAULT 0,
  consumed        INTEGER NOT NULL DEFAULT 0,
  expires_at      TEXT,
  created_at      TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_rules_match ON rules(event_type, consumed);

CREATE TABLE IF NOT EXISTS sessions (
  session_id TEXT PRIMARY KEY,
  agent      TEXT NOT NULL,
  repo_path  TEXT NOT NULL,
  state      TEXT NOT NULL DEFAULT 'waiting',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS continuations (
  id             TEXT PRIMARY KEY,
  rule_id        TEXT NOT NULL,
  event_id       TEXT NOT NULL,
  session_id     TEXT NOT NULL,
  prompt         TEXT NOT NULL,
  state          TEXT NOT NULL DEFAULT 'pending',
  command        TEXT NOT NULL DEFAULT '',
  exit_code      INTEGER,
  output_summary TEXT NOT NULL DEFAULT '',
  created_at     TEXT NOT NULL,
  updated_at     TEXT NOT NULL,
  UNIQUE(rule_id, event_id)
);
CREATE INDEX IF NOT EXISTS idx_continuations_state ON continuations(state);
`

func (s *Store) migrate() error {
	_, err := s.db.Exec(schema)
	return err
}

// ts/parseTS: all times stored as RFC3339Nano UTC strings.
func ts(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func parseTS(v string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, v)
	return t
}
