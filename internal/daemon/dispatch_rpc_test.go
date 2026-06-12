package daemon

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/tufantunc/RuntimePulse/internal/mockexe"
)

// mockAgent configures this test binary as the mocked agent CLI
// (re-exec pattern; see internal/mockexe) and points binEnv at it.
// Spec env vars are set with t.Setenv so spawned children inherit them.
func mockAgent(t *testing.T, binEnv string, spec mockexe.Spec) {
	t.Helper()
	for _, kv := range mockexe.Env(spec) {
		k, v, _ := strings.Cut(kv, "=")
		t.Setenv(k, v)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(binEnv, exe)
}

// mockClaude installs a fake claude binary for the test daemon.
func mockClaude(t *testing.T, spec mockexe.Spec) {
	t.Helper()
	mockAgent(t, "RUNTIMEPULSE_CLAUDE_BIN", spec)
}

func TestContinuationRunRPC(t *testing.T) {
	mockClaude(t, mockexe.Spec{Stdout: `{"result":"manual run ok"}`})
	_, c := startTestDaemon(t)

	if err := c.Call("session.register",
		map[string]any{"sessionId": "abc", "agent": "claude", "repoPath": t.TempDir()}, nil); err != nil {
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

func TestCursorSessionDispatchesViaMock(t *testing.T) {
	mockAgent(t, "RUNTIMEPULSE_CURSOR_BIN",
		mockexe.Spec{Stdout: `{"type":"result","result":"cursor turn done"}`})
	_, c := startTestDaemon(t)

	if err := c.Call("session.register",
		map[string]any{"sessionId": "cur-1", "agent": "cursor", "repoPath": t.TempDir()}, nil); err != nil {
		t.Fatal(err)
	}
	if err := c.Call("continuation.run",
		map[string]any{"sessionId": "cur-1", "prompt": "go"}, nil); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		var done []map[string]any
		if err := c.Call("continuation.list", map[string]any{"state": "completed"}, &done); err != nil {
			t.Fatal(err)
		}
		if len(done) == 1 {
			if s, _ := done[0]["outputSummary"].(string); s != "cursor turn done" {
				t.Fatalf("summary = %v", done[0])
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("cursor continuation never completed")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
