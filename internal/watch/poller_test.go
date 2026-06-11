package watch

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type recorder struct {
	mu     sync.Mutex
	events []string
}

func (r *recorder) emit(evType, source string, payload map[string]string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, evType)
}

func (r *recorder) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.events...)
}

func (r *recorder) waitLen(t *testing.T, n int) []string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if evs := r.snapshot(); len(evs) >= n {
			return evs
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d events, have %v", n, r.snapshot())
	return nil
}

func TestPollerInitialCheckAndEdge(t *testing.T) {
	var up atomic.Bool
	up.Store(true) // condition already holds at watch creation
	rec := &recorder{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go RunPoller(ctx, Config{Interval: 10 * time.Millisecond}, "up", "down", "x",
		func(context.Context) bool { return up.Load() }, rec.emit)

	evs := rec.waitLen(t, 1)
	if evs[0] != "up" {
		t.Fatalf("initial check must emit current state, got %v", evs)
	}

	up.Store(false) // transition
	evs = rec.waitLen(t, 2)
	if evs[1] != "down" {
		t.Fatalf("edge must emit transition, got %v", evs)
	}

	// steady state: no further events
	time.Sleep(50 * time.Millisecond)
	if evs := rec.snapshot(); len(evs) != 2 {
		t.Fatalf("steady state must not re-emit: %v", evs)
	}
}

func TestPollerStabilityThresholdSuppressesFlap(t *testing.T) {
	var up atomic.Bool
	up.Store(true)
	rec := &recorder{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go RunPoller(ctx, Config{Interval: 10 * time.Millisecond, StabilityThreshold: 100 * time.Millisecond},
		"up", "down", "x", func(context.Context) bool { return up.Load() }, rec.emit)
	rec.waitLen(t, 1) // initial "up"

	up.Store(false) // flap: down briefly, back up before threshold
	time.Sleep(30 * time.Millisecond)
	up.Store(true)
	time.Sleep(200 * time.Millisecond)
	if evs := rec.snapshot(); len(evs) != 1 {
		t.Fatalf("flap shorter than threshold must be suppressed: %v", evs)
	}

	up.Store(false) // real transition, held past threshold
	evs := rec.waitLen(t, 2)
	if evs[1] != "down" {
		t.Fatalf("held transition must emit: %v", evs)
	}
}
