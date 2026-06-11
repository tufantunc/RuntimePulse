package daemon

import (
	"net"
	"testing"
	"time"
)

func TestWatchRPCLifecycle(t *testing.T) {
	_, c := startTestDaemon(t)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	var w map[string]any
	if err := c.Call("watch.add", map[string]any{
		"type": "tcp", "target": ln.Addr().String(), "interval": "50ms",
	}, &w); err != nil {
		t.Fatal(err)
	}
	id, _ := w["id"].(string)
	if id == "" {
		t.Fatalf("watch.add returned no id: %v", w)
	}

	// invalid type rejected
	if err := c.Call("watch.add", map[string]any{"type": "nope", "target": "x"}, &map[string]any{}); err == nil {
		t.Fatal("invalid watch type must error")
	}

	// initial check produced a tcp.available event
	deadline := time.Now().Add(2 * time.Second)
	for {
		var evs []map[string]any
		if err := c.Call("events.list", map[string]any{"type": "tcp.available"}, &evs); err != nil {
			t.Fatal(err)
		}
		if len(evs) >= 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("initial tcp.available event never arrived")
		}
		time.Sleep(20 * time.Millisecond)
	}

	var list []map[string]any
	if err := c.Call("watch.list", nil, &list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("watch.list = %d, want 1", len(list))
	}

	if err := c.Call("watch.remove", map[string]any{"id": id}, nil); err != nil {
		t.Fatal(err)
	}
	list = nil
	if err := c.Call("watch.list", nil, &list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("watch not removed: %v", list)
	}
}
