package adapter

import (
	"context"
	"os"
	"os/exec"

	"github.com/tufantunc/RuntimePulse/internal/core"
)

// Claude resumes Claude Code sessions headlessly:
//
//	claude --resume <session-id> -p "<prompt>" --output-format json
//
// run in the session's repoPath (restores the original project config —
// see docs/reference/claude-code.md). v1 passes NO permission flags
// (owner decision): the agent runs with its project-configured
// permissions, and unapproved tool calls are denied in headless mode.
type Claude struct{}

func (Claude) Name() string { return "claude" }

func (Claude) Validate() error {
	_, err := exec.LookPath(claudeBin())
	return err
}

// claudeBin allows tests and the smoke script to substitute a mock
// binary; production uses "claude" from PATH.
func claudeBin() string {
	if b := os.Getenv("RUNTIMEPULSE_CLAUDE_BIN"); b != "" {
		return b
	}
	return "claude"
}

// BuildClaudeArgs builds the resume invocation. The prompt is a
// positional argument terminated by "--" so rendered prompts that start
// with a dash can never be eaten by claude's option parser (a "--help"
// prompt would otherwise exit 0 without resuming — a silent false
// success). --output-format must precede the terminator.
func BuildClaudeArgs(sessionID, prompt string) []string {
	return []string{"--resume", sessionID, "--output-format", "json", "-p", "--", prompt}
}

func (c Claude) Resume(ctx context.Context, sess core.Session, prompt string) (Result, error) {
	return runCLI(ctx, claudeBin(), BuildClaudeArgs(sess.SessionID, prompt), sess.RepoPath,
		func(out []byte) string { s, _ := ParseResultJSON(out); return s })
}
