package watch

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestManagerStartStop(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	rec := &recorder{}
	m := NewManager(rec.emit)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m.Run(ctx, nil)

	w := Watch{ID: "watch-1", Type: "tcp", Target: ln.Addr().String(),
		Config: Config{Interval: 20 * time.Millisecond}}
	if err := m.Start(w); err != nil {
		t.Fatal(err)
	}
	evs := rec.waitLen(t, 1) // initial check
	if evs[0] != "tcp.available" {
		t.Fatalf("want tcp.available, got %v", evs)
	}
	if got := m.RunningIDs(); len(got) != 1 || got[0] != "watch-1" {
		t.Fatalf("RunningIDs = %v", got)
	}

	// duplicate start is a no-op
	if err := m.Start(w); err != nil {
		t.Fatal(err)
	}
	if got := m.RunningIDs(); len(got) != 1 {
		t.Fatalf("duplicate start spawned a second watcher: %v", got)
	}

	m.Stop("watch-1")
	if got := m.RunningIDs(); len(got) != 0 {
		t.Fatalf("watch not stopped: %v", got)
	}

	ln.Close() // after stop: no more events even when state changes
	before := len(rec.snapshot())
	time.Sleep(100 * time.Millisecond)
	if after := len(rec.snapshot()); after != before {
		t.Fatal("stopped watch kept emitting")
	}
}

func TestManagerRunArmsPersisted(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	rec := &recorder{}
	m := NewManager(rec.emit)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m.Run(ctx, []Watch{{ID: "watch-9", Type: "tcp", Target: ln.Addr().String(),
		Config: Config{Interval: 20 * time.Millisecond}}})
	rec.waitLen(t, 1)
	if got := m.RunningIDs(); len(got) != 1 {
		t.Fatalf("persisted watch not armed: %v", got)
	}
}
