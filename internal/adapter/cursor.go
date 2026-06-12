package adapter

import (
	"context"
	"os"
	"os/exec"

	"github.com/tufantunc/RuntimePulse/internal/core"
)

// Cursor resumes Cursor CLI sessions headlessly:
//
//	agent -p --resume <chat-id> --output-format json -- "<prompt>"
//
// run in the session's repoPath (the CLI treats cwd as the repository
// root — see docs/reference/cursor-cli.md). The -p+--resume combination
// is not shown combined in official docs; Validate() existing is why.
// v1 passes NO permission flags (no --force/--trust — owner decision):
// in print mode the agent proposes edits without applying them unless
// the workspace allows it.
type Cursor struct{}

func (Cursor) Name() string { return "cursor" }

func (Cursor) Validate() error {
	_, err := exec.LookPath(cursorBin())
	return err
}

// cursorBin resolves the binary: env override → current name "agent" →
// legacy "cursor-agent" (pre-rename installs).
func cursorBin() string {
	if b := os.Getenv("RUNTIMEPULSE_CURSOR_BIN"); b != "" {
		return b
	}
	if _, err := exec.LookPath("agent"); err == nil {
		return "agent"
	}
	return "cursor-agent"
}

// BuildCursorArgs: the prompt is positional after "--" so dash-leading
// prompts can never be eaten by the option parser (stage-3 lesson).
func BuildCursorArgs(sessionID, prompt string) []string {
	return []string{"-p", "--resume", sessionID, "--output-format", "json", "--", prompt}
}

func (c Cursor) Resume(ctx context.Context, sess core.Session, prompt string) (Result, error) {
	return runCLI(ctx, cursorBin(), BuildCursorArgs(sess.SessionID, prompt), sess.RepoPath,
		func(out []byte) string { s, _ := ParseResultJSON(out); return s })
}
