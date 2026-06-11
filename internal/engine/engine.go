// Package engine wires the store's transactional ingest to the live bus.
package engine

import (
	"time"

	"github.com/tufantunc/RuntimePulse/internal/bus"
	"github.com/tufantunc/RuntimePulse/internal/core"
	"github.com/tufantunc/RuntimePulse/internal/store"
)

type Engine struct {
	Store *store.Store
	Bus   *bus.Bus
}

func New(st *store.Store, b *bus.Bus) *Engine {
	return &Engine{Store: st, Bus: b}
}

// Ingest normalizes the event, runs the outbox transaction, and only
// then notifies live subscribers (durable first, notify second).
func (e *Engine) Ingest(ev core.Event) (store.IngestResult, error) {
	if ev.ID == "" {
		ev.ID = core.NewID("evt")
	}
	if ev.Timestamp.IsZero() {
		ev.Timestamp = time.Now().UTC()
	}
	res, err := e.Store.Ingest(ev)
	if err != nil {
		return res, err
	}
	e.Bus.Publish(res.Event)
	return res, nil
}
