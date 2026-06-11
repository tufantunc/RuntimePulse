package bus

import (
	"testing"
	"time"

	"github.com/tufantunc/RuntimePulse/internal/core"
)

func TestPublishSubscribe(t *testing.T) {
	b := New()
	ch, cancel := b.Subscribe(4)
	defer cancel()
	b.Publish(core.Event{ID: "evt-1"})
	select {
	case ev := <-ch:
		if ev.ID != "evt-1" {
			t.Fatalf("got %q", ev.ID)
		}
	case <-time.After(time.Second):
		t.Fatal("event not delivered")
	}
}

func TestCancelClosesChannel(t *testing.T) {
	b := New()
	ch, cancel := b.Subscribe(1)
	cancel()
	if _, ok := <-ch; ok {
		t.Fatal("channel must be closed after cancel")
	}
	b.Publish(core.Event{ID: "evt-2"}) // must not panic
}

func TestSlowSubscriberDoesNotBlock(t *testing.T) {
	b := New()
	_, cancel := b.Subscribe(1) // never read
	defer cancel()
	done := make(chan struct{})
	go func() {
		for i := 0; i < 10; i++ {
			b.Publish(core.Event{ID: "evt"})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("publish blocked on a full subscriber")
	}
}
