package store

import (
	"testing"
	"time"

	"github.com/tufantunc/RuntimePulse/internal/core"
)

func testEvent(id string, at time.Time) core.Event {
	return core.Event{
		ID: id, Type: "docker.healthy", Source: "postgres",
		Payload:   map[string]string{"container": "postgres"},
		Timestamp: at,
	}
}

func TestInsertAndListEvents(t *testing.T) {
	s := openTestStore(t)
	now := time.Now().UTC()
	if err := s.InsertEvent(testEvent("evt-1", now)); err != nil {
		t.Fatal(err)
	}
	// duplicate id is ignored, not an error (idempotent re-ingest)
	if err := s.InsertEvent(testEvent("evt-1", now)); err != nil {
		t.Fatal(err)
	}
	if err := s.InsertEvent(testEvent("evt-2", now.Add(time.Second))); err != nil {
		t.Fatal(err)
	}

	evs, err := s.ListEvents("", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 2 {
		t.Fatalf("got %d events, want 2", len(evs))
	}
	if evs[0].ID != "evt-2" {
		t.Fatalf("newest first: got %s", evs[0].ID)
	}
	if evs[0].Payload["container"] != "postgres" {
		t.Fatalf("payload lost: %#v", evs[0].Payload)
	}

	byType, err := s.ListEvents("http.available", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(byType) != 0 {
		t.Fatalf("type filter failed, got %d", len(byType))
	}
}

func TestPruneEvents(t *testing.T) {
	s := openTestStore(t)
	old := time.Now().UTC().Add(-48 * time.Hour)
	if err := s.InsertEvent(testEvent("evt-old", old)); err != nil {
		t.Fatal(err)
	}
	if err := s.InsertEvent(testEvent("evt-new", time.Now().UTC())); err != nil {
		t.Fatal(err)
	}
	n, err := s.PruneEvents(time.Now().UTC().Add(-24 * time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("pruned %d, want 1", n)
	}
}

func TestListEventsOrderAcrossPrecisionBoundaries(t *testing.T) {
	s := openTestStore(t)
	base := time.Date(2026, 6, 11, 10, 0, 0, 0, time.UTC)
	// Trailing-zero trimming in RFC3339Nano would misorder these.
	s.InsertEvent(testEvent("evt-a", base))                           // 10:00:00
	s.InsertEvent(testEvent("evt-b", base.Add(100*time.Millisecond))) // 10:00:00.1
	s.InsertEvent(testEvent("evt-c", base.Add(120*time.Millisecond))) // 10:00:00.12
	s.InsertEvent(testEvent("evt-d", base.Add(time.Second)))          // 10:00:01
	evs, err := s.ListEvents("", 10)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"evt-d", "evt-c", "evt-b", "evt-a"} // newest first
	for i, w := range want {
		if evs[i].ID != w {
			t.Fatalf("position %d: got %s want %s (full: %v)", i, evs[i].ID, w, ids(evs))
		}
	}
}

func ids(evs []core.Event) []string {
	out := make([]string, len(evs))
	for i, e := range evs {
		out[i] = e.ID
	}
	return out
}
