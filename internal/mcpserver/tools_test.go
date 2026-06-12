package mcpserver

import (
	"context"
	"os"
	"testing"

	"github.com/tufantunc/RuntimePulse/internal/client"
	"github.com/tufantunc/RuntimePulse/internal/daemon"
)

// newTestDaemonClient starts a daemon in a short tmp dir and returns a
// connected client. macOS unix socket paths are ~104 bytes, so use /tmp.
func newTestDaemonClient(t *testing.T) *client.Client {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "rpmcp")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	d, err := daemon.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); d.Close() })
	go d.Serve(ctx)
	return client.New(daemon.SocketPath(dir))
}

func TestCreateWatchTool(t *testing.T) {
	c := newTestDaemonClient(t)
	h := createWatchHandler(c)
	_, out, err := h(context.Background(), nil, CreateWatchInput{
		Type: "tcp", Target: "localhost:5432", Interval: "1s",
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.ID == "" || out.Type != "tcp" {
		t.Fatalf("watch not created: %#v", out)
	}
	// invalid type → tool error (non-nil err)
	if _, _, err := h(context.Background(), nil, CreateWatchInput{Type: "nope", Target: "x"}); err == nil {
		t.Fatal("invalid watch type must surface as a tool error")
	}
}

func TestGetEventsTool(t *testing.T) {
	c := newTestDaemonClient(t)
	if err := c.Call("event.inject", map[string]any{"type": "build.failed", "source": "ci"}, nil); err != nil {
		t.Fatal(err)
	}
	h := getEventsHandler(c)
	_, out, err := h(context.Background(), nil, GetEventsInput{Type: "build.failed", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Events) != 1 || out.Events[0].Type != "build.failed" {
		t.Fatalf("get_events wrong: %#v", out.Events)
	}
}

func TestCancelRuleTool(t *testing.T) {
	c := newTestDaemonClient(t)
	if err := c.Call("session.register",
		map[string]any{"sessionId": "s1", "agent": "claude", "repoPath": "/tmp"}, nil); err != nil {
		t.Fatal(err)
	}
	var rule map[string]any
	if err := c.Call("rule.add", map[string]any{
		"type": "tcp.available", "sessionId": "s1", "prompt": "go",
	}, &rule); err != nil {
		t.Fatal(err)
	}
	id, _ := rule["id"].(string)
	h := cancelRuleHandler(c)
	if _, _, err := h(context.Background(), nil, CancelRuleInput{RuleID: id}); err != nil {
		t.Fatal(err)
	}
	var rules []map[string]any
	if err := c.Call("rule.list", map[string]any{"all": false}, &rules); err != nil {
		t.Fatal(err)
	}
	if len(rules) != 0 {
		t.Fatalf("rule not cancelled: %v", rules)
	}
}
