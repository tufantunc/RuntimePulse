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
	s.InsertEvent(testEvent("evt-old", old))
	s.InsertEvent(testEvent("evt-new", time.Now().UTC()))
	n, err := s.PruneEvents(time.Now().UTC().Add(-24 * time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("pruned %d, want 1", n)
	}
}
