package setup

import (
	"bytes"
	"os/exec"
	"strings"
)

// runFunc runs an agent CLI subcommand; injectable for tests.
type runFunc func(name string, args ...string) error

// execRun is the production runFunc.
func execRun(name string, args ...string) error {
	return exec.Command(name, args...).Run()
}

// queryFunc returns an agent CLI subcommand's combined output; injectable.
type queryFunc func(name string, args ...string) (string, error)

func execQuery(name string, args ...string) (string, error) {
	var buf bytes.Buffer
	cmd := exec.Command(name, args...)
	cmd.Stdout, cmd.Stderr = &buf, &buf
	err := cmd.Run()
	return buf.String(), err
}

// --- Claude: `claude mcp add` (both scopes) ---

type claudeAgent struct {
	run   runFunc   // nil → execRun
	query queryFunc // nil → execQuery
}

func (claudeAgent) Name() string { return "claude" }

func (claudeAgent) SupportsScope(Scope) bool { return true }

func (a claudeAgent) installed() bool {
	_, err := exec.LookPath("claude")
	return err == nil
}

func (a claudeAgent) Status(scope Scope) Status {
	st := Status{Installed: a.installed()}
	if !st.Installed {
		return st
	}
	q := a.query
	if q == nil {
		q = execQuery
	}
	out, err := q("claude", "mcp", "list")
	st.Registered = err == nil && strings.Contains(out, "runtimepulse")
	return st
}

func (a claudeAgent) Register(binPath string, scope Scope) error {
	run := a.run
	if run == nil {
		run = execRun
	}
	scopeName := "user"
	if scope == ScopeProject {
		scopeName = "project"
	}
	return run("claude", "mcp", "add", "--transport", "stdio", "--scope", scopeName,
		"runtimepulse", "--", binPath, "mcp")
}

// --- Codex: `codex mcp add` (global only in v1) ---

type codexAgent struct {
	run   runFunc
	query queryFunc
}

func (codexAgent) Name() string { return "codex" }

// SupportsScope: codex's project config is TOML; v1 delegates entirely
// to `codex mcp add` (global), so project scope is unsupported.
func (codexAgent) SupportsScope(scope Scope) bool { return scope == ScopeUser }

func (a codexAgent) installed() bool {
	_, err := exec.LookPath("codex")
	return err == nil
}

func (a codexAgent) Status(scope Scope) Status {
	st := Status{Installed: a.installed()}
	if !st.Installed {
		return st
	}
	q := a.query
	if q == nil {
		q = execQuery
	}
	out, err := q("codex", "mcp", "list")
	st.Registered = err == nil && strings.Contains(out, "runtimepulse")
	return st
}

func (a codexAgent) Register(binPath string, scope Scope) error {
	run := a.run
	if run == nil {
		run = execRun
	}
	return run("codex", "mcp", "add", "runtimepulse", "--", binPath, "mcp")
}
