package adapter

import (
	"context"
	"os"
	"os/exec"
	"strings"

	"github.com/tufantunc/RuntimePulse/internal/core"
)

// OpenCode resumes OpenCode sessions non-interactively:
//
//	opencode run --session <ses_id> -- "<prompt>"
//
// run in the session's repoPath (opencode stores sessions per-project
// keyed by directory — see docs/reference/opencode.md). Known caveat,
// documented there: opencode's CLI historically exits 0 on some errors;
// v1 trusts the exit code and records the output summary so failures
// are at least visible. The serve/HTTP path is a future improvement.
// v1 passes NO permission flags (owner decision).
type OpenCode struct{}

func (OpenCode) Name() string { return "opencode" }

func (OpenCode) Validate() error {
	_, err := exec.LookPath(openCodeBin())
	return err
}

func openCodeBin() string {
	if b := os.Getenv("RUNTIMEPULSE_OPENCODE_BIN"); b != "" {
		return b
	}
	return "opencode"
}

// BuildOpenCodeArgs: prompt positional after "--" (dash-safe).
func BuildOpenCodeArgs(sessionID, prompt string) []string {
	return []string{"run", "--session", sessionID, "--", prompt}
}

func (o OpenCode) Resume(ctx context.Context, sess core.Session, prompt string) (Result, error) {
	return runCLI(ctx, openCodeBin(), BuildOpenCodeArgs(sess.SessionID, prompt), sess.RepoPath,
		func(out []byte) string { return truncate(strings.TrimSpace(string(out))) })
}
