package watch

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

const gitDebounce = 300 * time.Millisecond

// RunGitWatch watches a repository's .git/HEAD (branch switches →
// git.branch.changed, payload branch) and .git/refs/heads + packed-refs
// (ref updates → git.ref.changed). There is no initial event: git
// watching is pure edge (no boolean state). A push is not reliably
// detectable locally, hence no git.pushed (spec note, §4.1).
// Blocks until ctx is cancelled.
func RunGitWatch(ctx context.Context, cfg Config, repo string, emit Emitter) error {
	gitDir := filepath.Join(repo, ".git")
	if fi, err := os.Stat(gitDir); err != nil || !fi.IsDir() {
		return fmt.Errorf("git watch: %s is not a git repository", repo)
	}
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer w.Close()
	if err := w.Add(gitDir); err != nil {
		return err
	}
	// refs/heads may not exist in a fresh repo; ignore add errors.
	w.Add(filepath.Join(gitDir, "refs", "heads")) //nolint:errcheck

	debounce := cfg.StabilityThreshold
	if debounce <= 0 {
		debounce = gitDebounce
	}
	var lastType string
	var lastAt time.Time

	currentBranch := func() string {
		b, err := os.ReadFile(filepath.Join(gitDir, "HEAD"))
		if err != nil {
			return ""
		}
		return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(b)), "ref: refs/heads/"))
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case ev, ok := <-w.Events:
			if !ok {
				return nil
			}
			var evType string
			payload := map[string]string{}
			base := filepath.Base(ev.Name)
			switch {
			case base == "HEAD" && filepath.Dir(ev.Name) == gitDir:
				evType = "git.branch.changed"
				payload["branch"] = currentBranch()
			case base == "packed-refs" || strings.Contains(ev.Name, filepath.Join("refs", "heads")):
				evType = "git.ref.changed"
				payload["ref"] = base
			default:
				continue
			}
			now := time.Now()
			if evType == lastType && now.Sub(lastAt) < debounce {
				continue
			}
			lastType, lastAt = evType, now
			emit(evType, repo, payload)
		case _, ok := <-w.Errors:
			if !ok {
				return nil
			}
		}
	}
}
