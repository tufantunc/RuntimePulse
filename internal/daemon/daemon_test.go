package daemon

import (
	"context"
	"os"
	"testing"

	"github.com/tufantunc/RuntimePulse/internal/client"
)

// Short tempdir: macOS unix socket paths are limited to ~104 bytes.
func shortTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "rp")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func startTestDaemon(t *testing.T) (*Daemon, *client.Client) {
	t.Helper()
	dir := shortTempDir(t)
	d, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); d.Close() })
	go d.Serve(ctx)
	return d, client.New(SocketPath(dir))
}

func TestStatusRPC(t *testing.T) {
	_, c := startTestDaemon(t)
	var st struct {
		Version string `json:"version"`
		Rules   int    `json:"rules"`
	}
	if err := c.Call("status", nil, &st); err != nil {
		t.Fatal(err)
	}
	if st.Version == "" {
		t.Fatal("status must report version")
	}
}

func TestRuleSessionInjectFlow(t *testing.T) {
	_, c := startTestDaemon(t)

	var sess map[string]any
	err := c.Call("session.register",
		map[string]any{"sessionId": "abc123", "agent": "claude", "repoPath": "/tmp/x"}, &sess)
	if err != nil {
		t.Fatal(err)
	}

	var rule map[string]any
	err = c.Call("rule.add", map[string]any{
		"type": "docker.healthy", "source": "postgres",
		"sessionId": "abc123", "prompt": "{{.Event.Source}} ready. Continue.", "oneShot": true,
	}, &rule)
	if err != nil {
		t.Fatal(err)
	}

	// rule.add against an unknown session must fail
	err = c.Call("rule.add", map[string]any{
		"type": "x", "sessionId": "missing", "prompt": "p",
	}, &map[string]any{})
	if err == nil {
		t.Fatal("rule.add for unregistered session must error")
	}

	var res struct {
		Continuations []map[string]any `json:"continuations"`
	}
	err = c.Call("event.inject",
		map[string]any{"type": "docker.healthy", "source": "postgres"}, &res)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Continuations) != 1 {
		t.Fatalf("inject produced %d continuations, want 1", len(res.Continuations))
	}
	if res.Continuations[0]["prompt"] != "postgres ready. Continue." {
		t.Fatalf("prompt = %v", res.Continuations[0]["prompt"])
	}

	var evs []map[string]any
	if err := c.Call("events.list", map[string]any{"limit": 10}, &evs); err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1 {
		t.Fatalf("events.list = %d, want 1", len(evs))
	}
}

func TestSocketPermissions(t *testing.T) {
	d, _ := startTestDaemon(t)
	info, err := os.Stat(SocketPath(d.Dir))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("socket perm = %o, want 0600", perm)
	}
}
