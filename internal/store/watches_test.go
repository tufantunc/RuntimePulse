package store

import (
	"testing"
	"time"

	"github.com/tufantunc/RuntimePulse/internal/watch"
)

func TestAddWatchIdempotent(t *testing.T) {
	s := openTestStore(t)
	w1, err := s.AddWatch(watch.Watch{Type: "http", Target: "http://localhost:3000"})
	if err != nil {
		t.Fatal(err)
	}
	if w1.ID == "" || w1.CreatedAt.IsZero() {
		t.Fatalf("AddWatch must fill id/created_at: %#v", w1)
	}
	// same (type, target) returns the existing watch, no duplicate
	w2, err := s.AddWatch(watch.Watch{Type: "http", Target: "http://localhost:3000",
		Config: watch.Config{Interval: 9 * time.Second}})
	if err != nil {
		t.Fatal(err)
	}
	if w2.ID != w1.ID {
		t.Fatalf("idempotency broken: %s != %s", w2.ID, w1.ID)
	}
	list, err := s.ListWatches()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("got %d watches, want 1", len(list))
	}

	if err := s.RemoveWatch(w1.ID); err != nil {
		t.Fatal(err)
	}
	list, _ = s.ListWatches()
	if len(list) != 0 {
		t.Fatal("watch not removed")
	}
}

func TestWatchConfigRoundTrip(t *testing.T) {
	s := openTestStore(t)
	w, err := s.AddWatch(watch.Watch{Type: "tcp", Target: "localhost:5432",
		Config: watch.Config{Interval: time.Second, StabilityThreshold: 3 * time.Second}})
	if err != nil {
		t.Fatal(err)
	}
	list, _ := s.ListWatches()
	if list[0].Config.Interval != time.Second || list[0].Config.StabilityThreshold != 3*time.Second {
		t.Fatalf("config lost: %#v", list[0].Config)
	}
	_ = w
}
