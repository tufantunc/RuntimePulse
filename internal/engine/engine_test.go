package engine

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/tufantunc/RuntimePulse/internal/bus"
	"github.com/tufantunc/RuntimePulse/internal/core"
	"github.com/tufantunc/RuntimePulse/internal/store"
)

func TestIngestFillsFieldsAndPublishes(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	b := bus.New()
	e := New(st, b)

	ch, cancel := b.Subscribe(4)
	defer cancel()

	res, err := e.Ingest(core.Event{Type: "http.available", Source: "localhost:3000"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Event.ID == "" || res.Event.Timestamp.IsZero() {
		t.Fatalf("engine must fill id/timestamp: %#v", res.Event)
	}
	select {
	case got := <-ch:
		if got.ID != res.Event.ID {
			t.Fatalf("published %q, ingested %q", got.ID, res.Event.ID)
		}
	case <-time.After(time.Second):
		t.Fatal("event not published to bus")
	}
}
