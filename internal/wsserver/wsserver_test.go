package wsserver

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/tufantunc/RuntimePulse/internal/bus"
	"github.com/tufantunc/RuntimePulse/internal/core"
)

func startTestWS(t *testing.T) (string, *bus.Bus) {
	t.Helper()
	b := bus.New()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	addr, err := Start(ctx, 0, "sekrit", b) // port 0 = ephemeral
	if err != nil {
		t.Fatal(err)
	}
	return addr, b
}

func TestStreamDeliversBusEvents(t *testing.T) {
	addr, b := startTestWS(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, fmt.Sprintf("ws://%s/events?token=sekrit", addr), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()

	time.Sleep(100 * time.Millisecond) // let the subscription arm
	b.Publish(core.Event{ID: "evt-1", Type: "tcp.available", Source: "x"})

	var ev core.Event
	if err := wsjson.Read(ctx, conn, &ev); err != nil {
		t.Fatal(err)
	}
	if ev.ID != "evt-1" || ev.Type != "tcp.available" {
		t.Fatalf("wrong event: %#v", ev)
	}
}

func TestWrongTokenRejected(t *testing.T) {
	addr, _ := startTestWS(t)
	resp, err := http.Get(fmt.Sprintf("http://%s/events?token=wrong", addr))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong token must 401, got %d", resp.StatusCode)
	}
	resp, err = http.Get(fmt.Sprintf("http://%s/events", addr)) // no token at all
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("missing token must 401, got %d", resp.StatusCode)
	}
}

func TestBearerHeaderAccepted(t *testing.T) {
	addr, _ := startTestWS(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, fmt.Sprintf("ws://%s/events", addr), &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": []string{"Bearer sekrit"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	conn.CloseNow()
}

func TestShutdownOnCtxCancel(t *testing.T) {
	b := bus.New()
	ctx, cancel := context.WithCancel(context.Background())
	addr, err := Start(ctx, 0, "tok", b)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := http.Get(fmt.Sprintf("http://%s/events", addr)); err != nil {
			return // server is down — good
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("server still answering after ctx cancel")
}
