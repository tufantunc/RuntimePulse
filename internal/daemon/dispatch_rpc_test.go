package daemon

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// mockClaude installs a fake claude binary for the test daemon.
func mockClaude(t *testing.T, script string) {
	t.Helper()
	dir := shortTempDir(t)
	mock := filepath.Join(dir, "mock-claude")
	if err := os.WriteFile(mock, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RUNTIMEPULSE_CLAUDE_BIN", mock)
}

func TestContinuationRunRPC(t *testing.T) {
	mockClaude(t, "#!/bin/sh\necho '{\"result\":\"manual run ok\"}'\n")
	_, c := startTestDaemon(t)

	if err := c.Call("session.register",
		map[string]any{"sessionId": "abc", "agent": "claude", "repoPath": "/tmp"}, nil); err != nil {
		t.Fatal(err)
	}
	var cont map[string]any
	if err := c.Call("continuation.run",
		map[string]any{"sessionId": "abc", "prompt": "do the thing"}, &cont); err != nil {
		t.Fatal(err)
	}
	if cont["id"] == "" {
		t.Fatalf("continuation.run returned no id: %v", cont)
	}

	deadline := time.Now().Add(3 * time.Second)
	for {
		var done []map[string]any
		if err := c.Call("continuation.list", map[string]any{"state": "completed"}, &done); err != nil {
			t.Fatal(err)
		}
		if len(done) == 1 {
			if s, _ := done[0]["outputSummary"].(string); s != "manual run ok" {
				t.Fatalf("summary = %v", done[0])
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("manual continuation never completed")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestContinuationRunUnknownSession(t *testing.T) {
	_, c := startTestDaemon(t)
	err := c.Call("continuation.run", map[string]any{"sessionId": "ghost", "prompt": "x"}, nil)
	if err == nil {
		t.Fatal("continuation.run for unregistered session must error")
	}
}
