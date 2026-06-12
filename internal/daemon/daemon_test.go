package daemon

import (
	"context"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/tufantunc/RuntimePulse/internal/client"
	"github.com/tufantunc/RuntimePulse/internal/mockexe"
)

// TestMain lets this test binary double as the mocked agent CLI
// (re-exec pattern; see internal/mockexe). Main is a no-op unless the
// mockexe sentinel env var marks the process as a spawned child.
func TestMain(m *testing.M) {
	mockexe.Main()
	os.Exit(m.Run())
}

// shortTempDir returns a temp dir whose path is short enough for unix
// socket limits (~104 bytes on macOS). On Windows the default temp dir
// is fine; on unix t.TempDir can exceed the limit, so use /tmp.
func shortTempDir(t *testing.T) string {
	t.Helper()
	root := "/tmp"
	if runtime.GOOS == "windows" {
		root = "" // os default (%TEMP%)
	}
	dir, err := os.MkdirTemp(root, "rp")
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
		map[string]any{"sessionId": "abc123", "agent": "claude", "repoPath": t.TempDir()}, &sess)
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

func TestRuleAddInvalidTemplateHasNoSideEffects(t *testing.T) {
	_, c := startTestDaemon(t)
	err := c.Call("rule.add", map[string]any{
		"type": "docker.healthy", "sessionId": "side-effect-test",
		"agent": "claude", "repoPath": t.TempDir(),
		"prompt": "{{.Event.Source", // syntax error
	}, &map[string]any{})
	if err == nil {
		t.Fatal("bad template must fail rule.add")
	}
	var sessions []map[string]any
	if err := c.Call("session.list", nil, &sessions); err != nil {
		t.Fatal(err)
	}
	for _, s := range sessions {
		if s["sessionId"] == "side-effect-test" {
			t.Fatal("failed rule.add must not register the session")
		}
	}
}

func TestSocketPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("socket file permissions are not meaningful on Windows (ACL model)")
	}
	d, _ := startTestDaemon(t)
	info, err := os.Stat(SocketPath(d.Dir))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("socket perm = %o, want 0600", perm)
	}
}

func TestAcquireLockExclusive(t *testing.T) {
	dir := shortTempDir(t)
	f, err := acquireLock(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if second, err := acquireLock(dir); err == nil {
		second.Close()
		t.Fatal("second acquireLock on the same dir must fail while the first is held")
	}
}

func TestCloseWithoutServeReturnsPromptly(t *testing.T) {
	dir := shortTempDir(t)
	d, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	d.Close() // Serve never ran: must not stall on dispatchDone
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("Close without Serve stalled %v", elapsed)
	}
}
