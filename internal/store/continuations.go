package store

import (
	"database/sql"
	"errors"
	"time"

	"github.com/tufantunc/RuntimePulse/internal/core"
)

// ClaimContinuation atomically moves a continuation pending → running.
// Returns false if it was not pending (already claimed or finished) —
// the DB row is the single source of truth for claims.
func (s *Store) ClaimContinuation(id string) (bool, error) {
	res, err := s.db.Exec(
		`UPDATE continuations SET state = 'running', updated_at = ? WHERE id = ? AND state = 'pending'`,
		ts(time.Now().UTC()), id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// UpdateContinuationResult records a finished run.
func (s *Store) UpdateContinuationResult(id string, state core.ContinuationState,
	command string, exitCode *int, outputSummary string) error {
	var exit any
	if exitCode != nil {
		exit = *exitCode
	}
	_, err := s.db.Exec(
		`UPDATE continuations SET state = ?, command = ?, exit_code = ?, output_summary = ?, updated_at = ?
		 WHERE id = ?`,
		string(state), command, exit, outputSummary, ts(time.Now().UTC()), id)
	return err
}

// ResetRunningContinuations moves orphaned running rows back to pending
// (daemon crashed mid-run). At-least-once: a continuation whose result
// was lost may run twice; that beats a lost wake-up.
func (s *Store) ResetRunningContinuations() (int64, error) {
	res, err := s.db.Exec(
		`UPDATE continuations SET state = 'pending', updated_at = ? WHERE state = 'running'`,
		ts(time.Now().UTC()))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// PendingSessions returns the distinct session ids that have pending work.
func (s *Store) PendingSessions() ([]string, error) {
	rows, err := s.db.Query(
		`SELECT DISTINCT session_id FROM continuations WHERE state = 'pending' ORDER BY session_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// NextPendingForSession returns the oldest pending continuation for a
// session (FIFO order: created_at, then id).
func (s *Store) NextPendingForSession(sessionID string) (core.Continuation, bool, error) {
	row := s.db.QueryRow(
		`SELECT id, rule_id, event_id, session_id, prompt, label, state, command,
		        exit_code, output_summary, created_at, updated_at
		 FROM continuations WHERE session_id = ? AND state = 'pending'
		 ORDER BY created_at, id LIMIT 1`, sessionID)
	c, err := scanContinuation(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return core.Continuation{}, false, nil
		}
		return core.Continuation{}, false, err
	}
	return c, true, nil
}

func scanContinuation(r rowScanner) (core.Continuation, error) {
	var c core.Continuation
	var st, created, updated string
	var exit *int
	if err := r.Scan(&c.ID, &c.RuleID, &c.EventID, &c.SessionID, &c.Prompt, &c.Label,
		&st, &c.Command, &exit, &c.OutputSummary, &created, &updated); err != nil {
		return core.Continuation{}, err
	}
	c.State = core.ContinuationState(st)
	c.ExitCode = exit
	c.CreatedAt, c.UpdatedAt = parseTS(created), parseTS(updated)
	return c, nil
}

// InsertManualContinuation creates a rule-less pending continuation
// (the CLI `continue` command / continuation.run RPC).
func (s *Store) InsertManualContinuation(sessionID, prompt string) (core.Continuation, error) {
	now := time.Now().UTC()
	c := core.Continuation{
		ID: core.NewID("cont"), RuleID: core.NewID("manual"), EventID: core.NewID("manual"),
		SessionID: sessionID, Prompt: prompt, Label: "manual",
		State: core.ContinuationPending, CreatedAt: now, UpdatedAt: now,
	}
	_, err := s.db.Exec(`
		INSERT INTO continuations
		  (id, rule_id, event_id, session_id, prompt, label, state, output_summary, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?)`,
		c.ID, c.RuleID, c.EventID, c.SessionID, c.Prompt, c.Label, string(c.State), "", ts(now), ts(now))
	return c, err
}
