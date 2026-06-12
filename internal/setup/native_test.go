package setup

import (
	"strings"
	"testing"
)

// recordCmd captures the most recent command for assertions.
type recordCmd struct {
	name string
	args []string
	err  error
}

func (r *recordCmd) run(name string, args ...string) error {
	r.name, r.args = name, args
	return r.err
}

func TestClaudeRegisterUserScope(t *testing.T) {
	rec := &recordCmd{}
	a := claudeAgent{run: rec.run}
	if err := a.Register("/abs/runtimepulse", ScopeUser); err != nil {
		t.Fatal(err)
	}
	got := rec.name + " " + strings.Join(rec.args, " ")
	want := "claude mcp add --transport stdio --scope user runtimepulse -- /abs/runtimepulse mcp"
	if got != want {
		t.Fatalf("\n got: %s\nwant: %s", got, want)
	}
}

func TestClaudeRegisterProjectScope(t *testing.T) {
	rec := &recordCmd{}
	a := claudeAgent{run: rec.run}
	a.Register("/abs/runtimepulse", ScopeProject)
	if !contains(rec.args, "--scope") || !contains(rec.args, "project") {
		t.Fatalf("project scope not passed: %v", rec.args)
	}
}

func TestCodexRegisterArgsAndScope(t *testing.T) {
	rec := &recordCmd{}
	a := codexAgent{run: rec.run}
	if a.SupportsScope(ScopeProject) {
		t.Fatal("codex must not advertise project scope in v1")
	}
	if err := a.Register("/abs/runtimepulse", ScopeUser); err != nil {
		t.Fatal(err)
	}
	got := rec.name + " " + strings.Join(rec.args, " ")
	want := "codex mcp add runtimepulse -- /abs/runtimepulse mcp"
	if got != want {
		t.Fatalf("\n got: %s\nwant: %s", got, want)
	}
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}
