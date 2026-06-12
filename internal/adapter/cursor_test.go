package adapter

import (
	"context"
	"strings"
	"testing"

	"github.com/tufantunc/RuntimePulse/internal/core"
)

func TestBuildCursorArgs(t *testing.T) {
	args := BuildCursorArgs("chat-1", "Continue.")
	want := []string{"-p", "--resume", "chat-1", "--output-format", "json", "--", "Continue."}
	if len(args) != len(want) {
		t.Fatalf("args = %v", args)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Fatalf("args[%d] = %q, want %q", i, args[i], want[i])
		}
	}
}

func TestCursorResumeWithMockBinary(t *testing.T) {
	// echo the LAST argument back as the result — proves the dash-safe
	// prompt position in one test.
	bin := mockBin(t, "#!/bin/sh\nfor last; do :; done\nprintf '{\"type\":\"result\",\"result\":\"%s\"}' \"$last\"\n")
	t.Setenv("RUNTIMEPULSE_CURSOR_BIN", bin)

	c := Cursor{}
	if err := c.Validate(); err != nil {
		t.Fatalf("validate with mock bin: %v", err)
	}
	res, err := c.Resume(context.Background(),
		core.Session{SessionID: "chat-1", RepoPath: t.TempDir()}, "- dash bullet prompt")
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode != 0 || res.OutputSummary != "- dash bullet prompt" {
		t.Fatalf("dash prompt mangled or wrong exit: %#v", res)
	}
	if !strings.Contains(res.Command, "--resume chat-1") {
		t.Fatalf("command not recorded: %q", res.Command)
	}
}

func TestCursorBinFallsBackWithoutEnv(t *testing.T) {
	t.Setenv("RUNTIMEPULSE_CURSOR_BIN", "")
	bin := cursorBin()
	if bin != "agent" && bin != "cursor-agent" {
		t.Fatalf("unexpected cursor binary resolution: %q", bin)
	}
}
