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
	evs := rec.waitLen(t, 1)
	found := false
	for _, e := range evs {
		if e == "git.branch.changed" {
			found = true
		}
	}
	if !found {
		t.Fatalf("branch switch must emit git.branch.changed, got %v", evs)
	}
}
