package adapter

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tufantunc/RuntimePulse/internal/core"
)

func TestBuildClaudeArgs(t *testing.T) {
	args := BuildClaudeArgs("abc123", "Postgres ready. Continue.")
	want := []string{"--resume", "abc123", "-p", "Postgres ready. Continue.", "--output-format", "json"}
	if len(args) != len(want) {
		t.Fatalf("args = %v", args)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Fatalf("args[%d] = %q, want %q", i, args[i], want[i])
		}
	}
}

func TestParseClaudeOutput(t *testing.T) {
	sum, structured := ParseClaudeOutput([]byte(`{"result":"Migrations applied.","session_id":"abc","total_cost_usd":0.01}`))
	if !structured || sum != "Migrations applied." {
		t.Fatalf("json parse failed: %q %v", sum, structured)
	}
	sum, structured = ParseClaudeOutput([]byte("plain text error output"))
	if structured || sum != "plain text error output" {
		t.Fatalf("fallback failed: %q %v", sum, structured)
	}
	long := strings.Repeat("x", 2000)
	sum, _ = ParseClaudeOutput([]byte(long))
	if len(sum) != summaryLimit {
		t.Fatalf("summary not truncated: %d", len(sum))
	}
}

// TestClaudeResumeWithMockBinary exercises the real subprocess path
// hermetically via RUNTIMEPULSE_CLAUDE_BIN.
func TestClaudeResumeWithMockBinary(t *testing.T) {
	dir := t.TempDir()
	mock := filepath.Join(dir, "mock-claude")
	script := "#!/bin/sh\necho '{\"result\":\"ok from mock\"}'\n"
	if err := os.WriteFile(mock, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RUNTIMEPULSE_CLAUDE_BIN", mock)

	c := Claude{}
	if err := c.Validate(); err != nil {
		t.Fatalf("validate with mock bin: %v", err)
	}
	res, err := c.Resume(context.Background(),
		core.Session{SessionID: "abc", RepoPath: dir}, "go on")
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode != 0 || res.OutputSummary != "ok from mock" {
		t.Fatalf("unexpected result: %#v", res)
	}
	if !strings.Contains(res.Command, "--resume abc") {
		t.Fatalf("command not recorded: %q", res.Command)
	}
}

func TestClaudeResumeNonzeroExit(t *testing.T) {
	dir := t.TempDir()
	mock := filepath.Join(dir, "mock-claude")
	if err := os.WriteFile(mock, []byte("#!/bin/sh\necho 'denied' >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RUNTIMEPULSE_CLAUDE_BIN", mock)
	res, err := Claude{}.Resume(context.Background(),
		core.Session{SessionID: "abc", RepoPath: dir}, "go")
	if err != nil {
		t.Fatalf("nonzero exit is a Result, not an error: %v", err)
	}
	if res.ExitCode != 1 || !strings.Contains(res.OutputSummary, "denied") {
		t.Fatalf("failure not captured: %#v", res)
	}
}
