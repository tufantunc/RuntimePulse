package store

import (
	"encoding/json"
	"time"

	"github.com/tufantunc/RuntimePulse/internal/core"
	"github.com/tufantunc/RuntimePulse/internal/watch"
)

// AddWatch persists a watch; an existing watch with the same
// (type, target) is returned unchanged (idempotent create, spec §7.2).
func (s *Store) AddWatch(w watch.Watch) (watch.Watch, error) {
	if w.ID == "" {
		w.ID = core.NewID("watch")
	}
	w.CreatedAt = time.Now().UTC()
	cfg, err := json.Marshal(w.Config)
	if err != nil {
		return watch.Watch{}, err
	}
	if _, err := s.db.Exec(
		`INSERT OR IGNORE INTO watches (id, type, target, config, created_at) VALUES (?,?,?,?,?)`,
		w.ID, w.Type, w.Target, string(cfg), ts(w.CreatedAt)); err != nil {
		return watch.Watch{}, err
	}
	// Return whatever the table actually holds for (type, target).
	row := s.db.QueryRow(
		`SELECT id, type, target, config, created_at FROM watches WHERE type = ? AND target = ?`,
		w.Type, w.Target)
	return scanWatch(row)
}

func (s *Store) ListWatches() ([]watch.Watch, error) {
	rows, err := s.db.Query(`SELECT id, type, target, config, created_at FROM watches ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []watch.Watch
	for rows.Next() {
		w, err := scanWatch(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

func (s *Store) RemoveWatch(id string) error {
	_, err := s.db.Exec(`DELETE FROM watches WHERE id = ?`, id)
	return err
}

func scanWatch(r rowScanner) (watch.Watch, error) {
	var w watch.Watch
	var cfg, created string
	if err := r.Scan(&w.ID, &w.Type, &w.Target, &cfg, &created); err != nil {
		return watch.Watch{}, err
	}
	if err := json.Unmarshal([]byte(cfg), &w.Config); err != nil {
		return watch.Watch{}, err
	}
	w.CreatedAt = parseTS(created)
	return w, nil
}
