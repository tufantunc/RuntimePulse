package watch

import (
	"context"
	"os/exec"
	"testing"
	"time"
)

func gitCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestGitWatchBranchChange(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	gitCmd(t, dir, "init", "-b", "main")
	gitCmd(t, dir, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "--allow-empty", "-m", "x")

	rec := &recorder{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		if err := RunGitWatch(ctx, Config{}, dir, rec.emit); err != nil && ctx.Err() == nil {
			t.Errorf("RunGitWatch: %v", err)
		}
	}()
	time.Sleep(150 * time.Millisecond)

	gitCmd(t, dir, "checkout", "-b", "feature/x")
	// checkout -b writes BOTH HEAD and refs; their fsnotify events arrive
	// in nondeterministic order. Poll for the branch event specifically —
	// waiting for "any first event" raced git.ref.changed on CI.
	deadline := time.Now().Add(3 * time.Second)
	for {
		for _, e := range rec.snapshot() {
			if e == "git.branch.changed" {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("branch switch must emit git.branch.changed, got %v", rec.snapshot())
		}
		time.Sleep(20 * time.Millisecond)
	}
}
