package adapter

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/tufantunc/RuntimePulse/internal/core"
)

const summaryLimit = 500

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

func BuildClaudeArgs(sessionID, prompt string) []string {
	return []string{"--resume", sessionID, "-p", prompt, "--output-format", "json"}
}

// ParseClaudeOutput extracts the result text from --output-format json,
// falling back to the raw output. The boolean reports whether
// structured output was recognized.
func ParseClaudeOutput(out []byte) (string, bool) {
	var r struct {
		Result string `json:"result"`
	}
	if err := json.Unmarshal(out, &r); err == nil && r.Result != "" {
		return truncate(r.Result), true
	}
	return truncate(strings.TrimSpace(string(out))), false
}

func truncate(s string) string {
	if len(s) > summaryLimit {
		return s[:summaryLimit]
	}
	return s
}

func (c Claude) Resume(ctx context.Context, sess core.Session, prompt string) (Result, error) {
	bin := claudeBin()
	args := BuildClaudeArgs(sess.SessionID, prompt)
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = sess.RepoPath

	start := time.Now()
	out, err := cmd.CombinedOutput()
	res := Result{
		Command:    bin + " " + strings.Join(args, " "),
		DurationMs: time.Since(start).Milliseconds(),
	}
	summary, _ := ParseClaudeOutput(out)
	res.OutputSummary = summary

	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			res.ExitCode = ee.ExitCode() // agent ran and failed: a Result, not an error
			return res, nil
		}
		return res, err // could not even run (binary missing, spawn failure)
	}
	return res, nil
}
