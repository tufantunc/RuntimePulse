package store

import (
	"database/sql"
	"errors"
	"time"

	"github.com/tufantunc/RuntimePulse/internal/core"
)

// RegisterSession upserts a session; new sessions start as waiting.
// Re-registering preserves existing state and created_at; the returned
// session is re-read from the database so it never disagrees with it.
func (s *Store) RegisterSession(sess core.Session) (core.Session, error) {
	now := time.Now().UTC()
	if sess.State == "" {
		sess.State = core.SessionWaiting
	}
	_, err := s.db.Exec(`
		INSERT INTO sessions (session_id, agent, repo_path, state, created_at, updated_at)
		VALUES (?,?,?,?,?,?)
		ON CONFLICT(session_id) DO UPDATE SET
		  agent = excluded.agent, repo_path = excluded.repo_path, updated_at = excluded.updated_at`,
		sess.SessionID, sess.Agent, sess.RepoPath, string(sess.State), ts(now), ts(now))
	if err != nil {
		return core.Session{}, err
	}
	stored, _, err := s.GetSession(sess.SessionID)
	return stored, err
}

func (s *Store) GetSession(id string) (core.Session, bool, error) {
	row := s.db.QueryRow(
		`SELECT session_id, agent, repo_path, state, created_at, updated_at
		 FROM sessions WHERE session_id = ?`, id)
	sess, err := scanSession(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return core.Session{}, false, nil
		}
		return core.Session{}, false, err
	}
	return sess, true, nil
}

func (s *Store) ListSessions() ([]core.Session, error) {
	rows, err := s.db.Query(
		`SELECT session_id, agent, repo_path, state, created_at, updated_at
		 FROM sessions ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []core.Session
	for rows.Next() {
		sess, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sess)
	}
	return out, rows.Err()
}

func (s *Store) SetSessionState(id string, st core.SessionState) error {
	_, err := s.db.Exec(`UPDATE sessions SET state = ?, updated_at = ? WHERE session_id = ?`,
		string(st), ts(time.Now().UTC()), id)
	return err
}

type rowScanner interface{ Scan(dest ...any) error }

func scanSession(r rowScanner) (core.Session, error) {
	var sess core.Session
	var state, created, updated string
	if err := r.Scan(&sess.SessionID, &sess.Agent, &sess.RepoPath, &state, &created, &updated); err != nil {
		return core.Session{}, err
	}
	sess.State = core.SessionState(state)
	sess.CreatedAt, sess.UpdatedAt = parseTS(created), parseTS(updated)
	return sess, nil
}
