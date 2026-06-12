package store

import "time"

// RetentionPolicy bounds how long pruned state lives. Windows are fixed
// constants in the daemon (owner decision: no flags, YAGNI).
type RetentionPolicy struct {
	SessionIdle     time.Duration
	ContinuationAge time.Duration
	EventAge        time.Duration
}

// SweepResult reports what one retention sweep removed.
type SweepResult struct {
	Sessions      int64
	Continuations int64
	Events        int64
}

// Sweep garbage-collects expired state in ONE transaction:
//
//   - sessions idle past SessionIdle — but never one that is running or
//     referenced by an active (unconsumed, unexpired) rule: an armed
//     session must survive until its rule fires or expires;
//   - terminal (completed/failed) continuations older than
//     ContinuationAge — pending/running are never touched;
//   - events older than EventAge.
//
// The three prunes are independent by design: deleting a session does
// not cascade into its historical continuations (those age out on
// their own and are inert once terminal).
func (s *Store) Sweep(now time.Time, p RetentionPolicy) (SweepResult, error) {
	var res SweepResult
	tx, err := s.db.Begin()
	if err != nil {
		return res, err
	}
	defer tx.Rollback()

	nowTS := ts(now)

	r, err := tx.Exec(`
		DELETE FROM sessions
		WHERE updated_at < ?
		  AND state != 'running'
		  AND session_id NOT IN (
		    SELECT session_id FROM rules
		    WHERE consumed = 0 AND (expires_at IS NULL OR expires_at > ?)
		  )`,
		ts(now.Add(-p.SessionIdle)), nowTS)
	if err != nil {
		return res, err
	}
	if res.Sessions, err = r.RowsAffected(); err != nil {
		return res, err
	}

	r, err = tx.Exec(`
		DELETE FROM continuations
		WHERE state IN ('completed','failed') AND updated_at < ?`,
		ts(now.Add(-p.ContinuationAge)))
	if err != nil {
		return res, err
	}
	if res.Continuations, err = r.RowsAffected(); err != nil {
		return res, err
	}

	r, err = tx.Exec(`DELETE FROM events WHERE created_at < ?`, ts(now.Add(-p.EventAge)))
	if err != nil {
		return res, err
	}
	if res.Events, err = r.RowsAffected(); err != nil {
		return res, err
	}

	return res, tx.Commit()
}
