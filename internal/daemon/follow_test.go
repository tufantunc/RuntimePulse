package daemon

import (
	"context"
	"testing"
	"time"

	"github.com/tufantunc/RuntimePulse/internal/core"
)

func TestEventsFollow(t *testing.T) {
	d, c := startTestDaemon(t)

	got := make(chan core.Event, 4)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	go func() {
		_ = c.Follow(ctx, func(ev core.Event) { got <- ev })
	}()

	// Give the follower a beat to subscribe, then inject.
	time.Sleep(100 * time.Millisecond)
	if _, err := d.Engine.Ingest(core.Event{Type: "file.changed", Source: "dist/index.js"}); err != nil {
		t.Fatal(err)
	}

	select {
	case ev := <-got:
		if ev.Type != "file.changed" {
			t.Fatalf("got %q", ev.Type)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("follow did not deliver the event")
	}
}
