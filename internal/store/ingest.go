package store

import (
	"time"

	"github.com/tufantunc/RuntimePulse/internal/core"
)

// IngestResult reports what one event produced.
type IngestResult struct {
	Event         core.Event          `json:"event"`
	Continuations []core.Continuation `json:"continuations"`
}

// Ingest is the outbox transaction (spec §5): store the event, match
// active rules, render prompts, create pending continuations, and
// consume oneShot rules — atomically. (rule_id, event_id) uniqueness
// makes re-delivery of the same event a no-op.
//
// Everything runs on the tx: with SetMaxOpenConns(1), touching s.db
// while the tx is open would deadlock the connection pool.
func (s *Store) Ingest(ev core.Event) (IngestResult, error) {
	res := IngestResult{Event: ev}
	tx, err := s.db.Begin()
	if err != nil {
		return res, err
	}
	defer tx.Rollback()

	if err := insertEventTx(tx, ev); err != nil {
		return res, err
	}

	now := time.Now().UTC()
	rows, err := tx.Query(`
		SELECT id, event_type, event_source, action_kind, session_id,
		       prompt_template, label, one_shot, consumed, expires_at, created_at
		FROM rules
		WHERE event_type = ? AND (event_source = '' OR event_source = ?)
		  AND consumed = 0 AND (expires_at IS NULL OR expires_at > ?)`,
		ev.Type, ev.Source, ts(now))
	if err != nil {
		return res, err
	}
	var matched []core.Rule
	for rows.Next() {
		r, err := scanRule(rows)
		if err != nil {
			rows.Close()
			return res, err
		}
		matched = append(matched, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return res, err
	}

	for _, r := range matched {
		c := core.Continuation{
			ID: core.NewID("cont"), RuleID: r.ID, EventID: ev.ID, SessionID: r.SessionID,
			State: core.ContinuationPending, CreatedAt: now, UpdatedAt: now,
		}
		prompt, err := r.RenderPrompt(ev)
		if err != nil {
			c.State = core.ContinuationFailed
			c.OutputSummary = "prompt template error: " + err.Error()
		} else {
			c.Prompt = prompt
		}
		ins, err := tx.Exec(`
			INSERT OR IGNORE INTO continuations
			  (id, rule_id, event_id, session_id, prompt, state, output_summary, created_at, updated_at)
			VALUES (?,?,?,?,?,?,?,?,?)`,
			c.ID, c.RuleID, c.EventID, c.SessionID, c.Prompt, string(c.State),
			c.OutputSummary, ts(now), ts(now))
		if err != nil {
			return res, err
		}
		n, err := ins.RowsAffected()
		if err != nil {
			return res, err
		}
		if n == 0 {
			continue // this (rule, event) pair already produced a continuation
		}
		if r.OneShot {
			if _, err := tx.Exec(`UPDATE rules SET consumed = 1 WHERE id = ?`, r.ID); err != nil {
				return res, err
			}
		}
		res.Continuations = append(res.Continuations, c)
	}

	if err := tx.Commit(); err != nil {
		return res, err
	}
	return res, nil
}

// ListContinuations returns continuations, optionally filtered by state.
func (s *Store) ListContinuations(state string) ([]core.Continuation, error) {
	q := `SELECT id, rule_id, event_id, session_id, prompt, state, command,
	             exit_code, output_summary, created_at, updated_at
	      FROM continuations`
	args := []any{}
	if state != "" {
		q += ` WHERE state = ?`
		args = append(args, state)
	}
	q += ` ORDER BY created_at, id`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []core.Continuation
	for rows.Next() {
		var c core.Continuation
		var st, created, updated string
		var exit *int
		if err := rows.Scan(&c.ID, &c.RuleID, &c.EventID, &c.SessionID, &c.Prompt,
			&st, &c.Command, &exit, &c.OutputSummary, &created, &updated); err != nil {
			return nil, err
		}
		c.State = core.ContinuationState(st)
		c.ExitCode = exit
		c.CreatedAt, c.UpdatedAt = parseTS(created), parseTS(updated)
		out = append(out, c)
	}
	return out, rows.Err()
}
