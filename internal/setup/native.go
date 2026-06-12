package setup

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

// runFunc runs an agent CLI subcommand; injectable for tests.
type runFunc func(name string, args ...string) error

// execRun runs the command, folding its combined output into the error
// so a failed `mcp add` produces an actionable message, not "exit 1".
func execRun(name string, args ...string) error {
	out, err := execQuery(name, args...)
	if err != nil {
		msg := strings.TrimSpace(out)
		if msg == "" {
			return err
		}
		return fmt.Errorf("%w: %s", err, msg)
	}
	return nil
}

// tolerateExists returns nil when err describes an "already exists"
// condition; the spec requires this to be treated as success.
func tolerateExists(err error) error {
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "already exists") {
		return nil
	}
	return err
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
	err := run("claude", "mcp", "add", "--transport", "stdio", "--scope", scopeName,
		"runtimepulse", "--", binPath, "mcp")
	return tolerateExists(err)
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
	err := run("codex", "mcp", "add", "runtimepulse", "--", binPath, "mcp")
	return tolerateExists(err)
}
