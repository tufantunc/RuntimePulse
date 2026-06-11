package store

import (
	"database/sql"
	"encoding/json"
	"time"

	"github.com/tufantunc/RuntimePulse/internal/core"
)

// InsertEvent stores an event; re-inserting the same id is a no-op.
func (s *Store) InsertEvent(ev core.Event) error {
	return insertEventTx(s.db, ev)
}

// execer lets the same statements run on *sql.DB and *sql.Tx.
type execer interface {
	Exec(query string, args ...any) (sql.Result, error)
}

func insertEventTx(e execer, ev core.Event) error {
	payload, err := json.Marshal(ev.Payload)
	if err != nil {
		return err
	}
	_, err = e.Exec(
		`INSERT OR IGNORE INTO events (id, type, source, payload, created_at) VALUES (?,?,?,?,?)`,
		ev.ID, ev.Type, ev.Source, string(payload), ts(ev.Timestamp))
	return err
}

// ListEvents returns newest-first events, optionally filtered by type.
func (s *Store) ListEvents(evType string, limit int) ([]core.Event, error) {
	if limit <= 0 {
		limit = 100
	}
	q := `SELECT id, type, source, payload, created_at FROM events `
	args := []any{}
	if evType != "" {
		q += `WHERE type = ? `
		args = append(args, evType)
	}
	q += `ORDER BY created_at DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []core.Event
	for rows.Next() {
		var ev core.Event
		var payload, created string
		if err := rows.Scan(&ev.ID, &ev.Type, &ev.Source, &payload, &created); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(payload), &ev.Payload); err != nil {
			return nil, err
		}
		ev.Timestamp = parseTS(created)
		out = append(out, ev)
	}
	return out, rows.Err()
}

// PruneEvents deletes events older than the cutoff, returning the count.
func (s *Store) PruneEvents(before time.Time) (int64, error) {
	res, err := s.db.Exec(`DELETE FROM events WHERE created_at < ?`, ts(before))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
