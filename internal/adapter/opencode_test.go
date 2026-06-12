package adapter

import (
	"context"
	"testing"

	"github.com/tufantunc/RuntimePulse/internal/core"
)

func TestBuildOpenCodeArgs(t *testing.T) {
	args := BuildOpenCodeArgs("ses_abc", "Continue.")
	want := []string{"run", "--session", "ses_abc", "--", "Continue."}
	if len(args) != len(want) {
		t.Fatalf("args = %v", args)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Fatalf("args[%d] = %q, want %q", i, args[i], want[i])
		}
	}
}

func TestOpenCodeResumeWithMockBinary(t *testing.T) {
	bin := mockBin(t, "#!/bin/sh\nfor last; do :; done\necho \"oc: $last\"\n")
	t.Setenv("RUNTIMEPULSE_OPENCODE_BIN", bin)

	o := OpenCode{}
	if err := o.Validate(); err != nil {
		t.Fatal(err)
	}
	res, err := o.Resume(context.Background(),
		core.Session{SessionID: "ses_abc", RepoPath: t.TempDir()}, "-leading dash")
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode != 0 || res.OutputSummary != "oc: -leading dash" {
		t.Fatalf("dash prompt mangled or wrong exit: %#v", res)
	}
}
