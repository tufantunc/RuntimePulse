package adapter

import (
	"context"
	"os"
	"os/exec"
	"strings"

	"github.com/tufantunc/RuntimePulse/internal/core"
)

// Codex resumes Codex CLI sessions non-interactively:
//
//	codex exec resume <session-id> --skip-git-repo-check -- "<prompt>"
//
// run in the session's repoPath. Default exec mode streams progress to
// stderr and prints only the final agent message to stdout (see
// docs/reference/codex-cli.md) — the shared runner keeps them apart.
// v1 passes NO sandbox/approval flags (owner decision); codex exec
// defaults to a read-only sandbox, a documented limitation.
type Codex struct{}

func (Codex) Name() string { return "codex" }

func (Codex) Validate() error {
	_, err := exec.LookPath(codexBin())
	return err
}

func codexBin() string {
	if b := os.Getenv("RUNTIMEPULSE_CODEX_BIN"); b != "" {
		return b
	}
	return "codex"
}

// BuildCodexArgs: prompt positional after "--" (dash-safe, stage-3
// lesson); --skip-git-repo-check keeps non-git repoPaths working.
func BuildCodexArgs(sessionID, prompt string) []string {
	return []string{"exec", "resume", sessionID, "--skip-git-repo-check", "--", prompt}
}

func (c Codex) Resume(ctx context.Context, sess core.Session, prompt string) (Result, error) {
	return runCLI(ctx, codexBin(), BuildCodexArgs(sess.SessionID, prompt), sess.RepoPath,
		func(out []byte) string { return truncate(strings.TrimSpace(string(out))) })
}
