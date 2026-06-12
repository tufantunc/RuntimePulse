package mcpserver

import (
	"context"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/tufantunc/RuntimePulse/internal/client"
	"github.com/tufantunc/RuntimePulse/internal/daemon"
)

// newTestDaemonClient starts a daemon in a short tmp dir and returns a
// connected client. macOS unix socket paths are ~104 bytes, so use /tmp
// on unix; on Windows the default temp dir is used.
func newTestDaemonClient(t *testing.T) *client.Client {
	t.Helper()
	root := "/tmp"
	if runtime.GOOS == "windows" {
		root = "" // os default (%TEMP%)
	}
	dir, err := os.MkdirTemp(root, "rpmcp")
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
		map[string]any{"sessionId": "s1", "agent": "claude", "repoPath": t.TempDir()}, nil); err != nil {
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

func TestCreateRuleSelfRegisters(t *testing.T) {
	c := newTestDaemonClient(t)
	h := createRuleHandler(c)
	_, out, err := h(context.Background(), nil, CreateRuleInput{
		EventType: "docker.healthy", Source: "postgres",
		SessionID: "abc123", Agent: "claude", RepoPath: t.TempDir(),
		Prompt: "{{.Event.Source}} healthy. Continue.", Label: "step-1", OneShot: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.ID == "" {
		t.Fatalf("rule not created: %#v", out)
	}
	// session was registered as a side effect
	var sessions []map[string]any
	if err := c.Call("session.list", nil, &sessions); err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0]["sessionId"] != "abc123" {
		t.Fatalf("self-registration failed: %v", sessions)
	}
}

func TestCreateRuleBadTemplateIsToolError(t *testing.T) {
	c := newTestDaemonClient(t)
	h := createRuleHandler(c)
	_, _, err := h(context.Background(), nil, CreateRuleInput{
		EventType: "docker.healthy", SessionID: "abc123", Agent: "claude", RepoPath: t.TempDir(),
		Prompt: "{{.Event.Bad", // syntax error
	})
	if err == nil {
		t.Fatal("bad template must surface as a tool error")
	}
}

func TestWaitForEventReturnsLiveMatch(t *testing.T) {
	c := newTestDaemonClient(t)
	h := waitForEventHandler(c)

	got := make(chan WaitOutput, 1)
	go func() {
		_, out, err := h(context.Background(), nil, WaitForEventInput{
			EventType: "http.available", Source: "localhost:3000", TimeoutSeconds: 5,
		})
		if err != nil {
			t.Errorf("wait_for_event: %v", err)
		}
		got <- out
	}()

	time.Sleep(150 * time.Millisecond) // let the subscription arm
	if err := c.Call("event.inject",
		map[string]any{"type": "http.available", "source": "localhost:3000"}, nil); err != nil {
		t.Fatal(err)
	}

	select {
	case out := <-got:
		if out.TimedOut || out.Event.Type != "http.available" || out.Event.Source != "localhost:3000" {
			t.Fatalf("unexpected wait result: %#v", out)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("wait_for_event did not return on a live match")
	}
}

func TestWaitForEventTimesOut(t *testing.T) {
	c := newTestDaemonClient(t)
	h := waitForEventHandler(c)
	_, out, err := h(context.Background(), nil, WaitForEventInput{
		EventType: "never.happens", TimeoutSeconds: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !out.TimedOut {
		t.Fatalf("expected timeout, got %#v", out)
	}
}

func TestWaitForEventFiltersSource(t *testing.T) {
	c := newTestDaemonClient(t)
	h := waitForEventHandler(c)
	got := make(chan WaitOutput, 1)
	go func() {
		_, out, _ := h(context.Background(), nil, WaitForEventInput{
			EventType: "docker.healthy", Source: "postgres", TimeoutSeconds: 5,
		})
		got <- out
	}()
	time.Sleep(150 * time.Millisecond)
	c.Call("event.inject", map[string]any{"type": "docker.healthy", "source": "redis"}, nil)    // non-match
	c.Call("event.inject", map[string]any{"type": "docker.healthy", "source": "postgres"}, nil) // match
	select {
	case out := <-got:
		if out.TimedOut || out.Event.Source != "postgres" {
			t.Fatalf("source filter failed: %#v", out)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("wait_for_event did not return")
	}
}
