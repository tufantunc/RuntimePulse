package adapter

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/tufantunc/RuntimePulse/internal/core"
	"github.com/tufantunc/RuntimePulse/internal/mockexe"
)

func TestBuildClaudeArgs(t *testing.T) {
	args := BuildClaudeArgs("abc123", "Postgres ready. Continue.")
	want := []string{"--resume", "abc123", "--output-format", "json", "-p", "--", "Postgres ready. Continue."}
	if len(args) != len(want) {
		t.Fatalf("args = %v", args)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Fatalf("args[%d] = %q, want %q", i, args[i], want[i])
		}
	}
}

func TestParseResultJSONClaudeShape(t *testing.T) {
	sum, structured := ParseResultJSON([]byte(`{"result":"Migrations applied.","session_id":"abc","total_cost_usd":0.01}`))
	if !structured || sum != "Migrations applied." {
		t.Fatalf("json parse failed: %q %v", sum, structured)
	}
	sum, structured = ParseResultJSON([]byte("plain text error output"))
	if structured || sum != "plain text error output" {
		t.Fatalf("fallback failed: %q %v", sum, structured)
	}
	long := strings.Repeat("x", 2000)
	sum, _ = ParseResultJSON([]byte(long))
	if len(sum) != summaryLimit {
		t.Fatalf("summary not truncated: %d", len(sum))
	}
}

// TestClaudeResumeWithMockBinary exercises the real subprocess path
// hermetically via RUNTIMEPULSE_CLAUDE_BIN.
func TestClaudeResumeWithMockBinary(t *testing.T) {
	mock := mockBin(t, mockexe.Spec{Stdout: `{"result":"ok from mock"}`})
	t.Setenv("RUNTIMEPULSE_CLAUDE_BIN", mock)

	c := Claude{}
	if err := c.Validate(); err != nil {
		t.Fatalf("validate with mock bin: %v", err)
	}
	res, err := c.Resume(context.Background(),
		core.Session{SessionID: "abc", RepoPath: t.TempDir()}, "go on")
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
	mock := mockBin(t, mockexe.Spec{Stderr: "denied", Exit: 1})
	t.Setenv("RUNTIMEPULSE_CLAUDE_BIN", mock)
	res, err := Claude{}.Resume(context.Background(),
		core.Session{SessionID: "abc", RepoPath: t.TempDir()}, "go")
	if err != nil {
		t.Fatalf("nonzero exit is a Result, not an error: %v", err)
	}
	if res.ExitCode != 1 || !strings.Contains(res.OutputSummary, "denied") {
		t.Fatalf("failure not captured: %#v", res)
	}
}

func TestClaudeResumeDashPromptArrivesIntact(t *testing.T) {
	// echo the LAST argument (the prompt, after the `--` terminator)
	// back as the result
	mock := mockBin(t, mockexe.Spec{Stdout: `{"result":"{LAST}"}`})
	t.Setenv("RUNTIMEPULSE_CLAUDE_BIN", mock)
	res, err := Claude{}.Resume(context.Background(),
		core.Session{SessionID: "abc", RepoPath: t.TempDir()}, "- Postgres is ready. Continue.")
	if err != nil {
		t.Fatal(err)
	}
	if res.OutputSummary != "- Postgres is ready. Continue." {
		t.Fatalf("dash prompt mangled: %q", res.OutputSummary)
	}
}

func TestClaudeResumeTimeoutAnnotated(t *testing.T) {
	mock := mockBin(t, mockexe.Spec{DelayMs: 5000})
	t.Setenv("RUNTIMEPULSE_CLAUDE_BIN", mock)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	res, err := Claude{}.Resume(ctx, core.Session{SessionID: "abc", RepoPath: t.TempDir()}, "go")
	if err != nil {
		t.Fatalf("timeout is a Result, not an error: %v", err)
	}
	if res.ExitCode != -1 || !strings.Contains(res.OutputSummary, "timed out or cancelled") {
		t.Fatalf("timeout not annotated: %#v", res)
	}
}
