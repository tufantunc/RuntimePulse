package adapter

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/tufantunc/RuntimePulse/internal/mockexe"
)

func TestMain(m *testing.M) {
	mockexe.Main()
	os.Exit(m.Run())
}

// mockBin configures this test binary as the mocked CLI (re-exec
// pattern; see internal/mockexe) and returns its path. Spec env vars
// are set with t.Setenv so spawned children inherit them.
func mockBin(t *testing.T, spec mockexe.Spec) string {
	t.Helper()
	for _, kv := range mockexe.Env(spec) {
		k, v, _ := strings.Cut(kv, "=")
		t.Setenv(k, v)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return exe
}

func rawSummary(out []byte) string { return truncate(strings.TrimSpace(string(out))) }

func TestRunCLISuccessParsesStdoutOnly(t *testing.T) {
	bin := mockBin(t, mockexe.Spec{Stdout: "final answer", Stderr: "progress noise"})
	res, err := runCLI(context.Background(), bin, []string{"a"}, t.TempDir(), rawSummary)
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode != 0 || res.OutputSummary != "final answer" {
		t.Fatalf("stderr leaked into summary or wrong exit: %#v", res)
	}
	if !strings.Contains(res.Command, bin+" a") {
		t.Fatalf("command not recorded: %q", res.Command)
	}
}

func TestRunCLIStderrFallbackWhenStdoutEmpty(t *testing.T) {
	bin := mockBin(t, mockexe.Spec{Stderr: "boom: session not found", Exit: 3})
	res, err := runCLI(context.Background(), bin, nil, t.TempDir(), rawSummary)
	if err != nil {
		t.Fatal(err) // nonzero exit is a Result, not an error
	}
	if res.ExitCode != 3 || !strings.Contains(res.OutputSummary, "session not found") {
		t.Fatalf("stderr fallback failed: %#v", res)
	}
}

func TestRunCLITimeoutAnnotated(t *testing.T) {
	bin := mockBin(t, mockexe.Spec{DelayMs: 5000})
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	res, err := runCLI(ctx, bin, nil, t.TempDir(), rawSummary)
	if err != nil {
		t.Fatalf("timeout is a Result, not an error: %v", err)
	}
	if res.ExitCode != -1 || !strings.Contains(res.OutputSummary, "timed out or cancelled") {
		t.Fatalf("timeout not annotated: %#v", res)
	}
}

func TestRunCLISpawnFailureIsError(t *testing.T) {
	if _, err := runCLI(context.Background(), "/definitely/missing/bin", nil, t.TempDir(), rawSummary); err == nil {
		t.Fatal("missing binary must be an error")
	}
}

func TestParseResultJSON(t *testing.T) {
	sum, ok := ParseResultJSON([]byte(`{"type":"result","result":"Done.","session_id":"x"}`))
	if !ok || sum != "Done." {
		t.Fatalf("json parse: %q %v", sum, ok)
	}
	sum, ok = ParseResultJSON([]byte("plain text"))
	if ok || sum != "plain text" {
		t.Fatalf("fallback: %q %v", sum, ok)
	}
}
