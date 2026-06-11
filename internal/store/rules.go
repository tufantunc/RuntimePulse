package store

import (
	"database/sql"
	"time"

	"github.com/tufantunc/RuntimePulse/internal/core"
)

// AddRule stores a rule, assigning id and created_at.
func (s *Store) AddRule(r core.Rule) (core.Rule, error) {
	if r.ID == "" {
		r.ID = core.NewID("rule")
	}
	if r.ActionKind == "" {
		r.ActionKind = core.ActionContinueSession
	}
	r.Consumed = false
	r.CreatedAt = time.Now().UTC()
	var expires any
	if r.ExpiresAt != nil {
		expires = ts(*r.ExpiresAt)
	}
	_, err := s.db.Exec(`
		INSERT INTO rules (id, event_type, event_source, action_kind, session_id,
		                   prompt_template, label, one_shot, consumed, expires_at, created_at)
		VALUES (?,?,?,?,?,?,?,?,0,?,?)`,
		r.ID, r.Selector.Type, r.Selector.Source, r.ActionKind, r.SessionID,
		r.PromptTemplate, r.Label, boolInt(r.OneShot), expires, ts(r.CreatedAt))
	return r, err
}

// ListRules returns rules; includeInactive=false filters out consumed and expired.
func (s *Store) ListRules(includeInactive bool) ([]core.Rule, error) {
	q := `SELECT id, event_type, event_source, action_kind, session_id,
	             prompt_template, label, one_shot, consumed, expires_at, created_at
	      FROM rules`
	args := []any{}
	if !includeInactive {
		q += ` WHERE consumed = 0 AND (expires_at IS NULL OR expires_at > ?)`
		args = append(args, ts(time.Now().UTC()))
	}
	q += ` ORDER BY created_at`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []core.Rule
	for rows.Next() {
		r, err := scanRule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) RemoveRule(id string) error {
	_, err := s.db.Exec(`DELETE FROM rules WHERE id = ?`, id)
	return err
}

func scanRule(r rowScanner) (core.Rule, error) {
	var rule core.Rule
	var oneShot, consumed int
	var expires sql.NullString
	var created string
	if err := r.Scan(&rule.ID, &rule.Selector.Type, &rule.Selector.Source, &rule.ActionKind,
		&rule.SessionID, &rule.PromptTemplate, &rule.Label, &oneShot, &consumed,
		&expires, &created); err != nil {
		return core.Rule{}, err
	}
	rule.OneShot, rule.Consumed = oneShot == 1, consumed == 1
	if expires.Valid {
		t := parseTS(expires.String)
		rule.ExpiresAt = &t
	}
	rule.CreatedAt = parseTS(created)
	return rule, nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
