package adapter

import (
	"context"
	"strings"
	"testing"

	"github.com/tufantunc/RuntimePulse/internal/core"
	"github.com/tufantunc/RuntimePulse/internal/mockexe"
)

func TestBuildCodexArgs(t *testing.T) {
	args := BuildCodexArgs("sess-9", "Continue.")
	want := []string{"exec", "resume", "sess-9", "--skip-git-repo-check", "--", "Continue."}
	if len(args) != len(want) {
		t.Fatalf("args = %v", args)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Fatalf("args[%d] = %q, want %q", i, args[i], want[i])
		}
	}
}

func TestCodexResumeSeparatesProgressFromResult(t *testing.T) {
	// codex semantics: progress → stderr, final message → stdout
	bin := mockBin(t, mockexe.Spec{Stderr: "thinking...", Stdout: "did: {LAST}"})
	t.Setenv("RUNTIMEPULSE_CODEX_BIN", bin)

	c := Codex{}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	res, err := c.Resume(context.Background(),
		core.Session{SessionID: "sess-9", RepoPath: t.TempDir()}, "--dash prompt")
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode != 0 || res.OutputSummary != "did: --dash prompt" {
		t.Fatalf("wrong summary (progress leaked, or dash prompt eaten): %#v", res)
	}
	if strings.Contains(res.OutputSummary, "thinking") {
		t.Fatal("stderr progress must not pollute the summary")
	}
}
