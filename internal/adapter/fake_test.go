package adapter

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/tufantunc/RuntimePulse/internal/core"
)

func TestFakeRecordsCallsAndControlsOutcome(t *testing.T) {
	f := NewFake()
	sess := core.Session{SessionID: "s1", Agent: "fake", RepoPath: t.TempDir()}

	res, err := f.Resume(context.Background(), sess, "do it")
	if err != nil || res.ExitCode != 0 {
		t.Fatalf("default fake must succeed: %#v %v", res, err)
	}
	f.SetOutcome(7, errors.New("boom"))
	res, err = f.Resume(context.Background(), sess, "again")
	if err == nil || res.ExitCode != 7 {
		t.Fatalf("configured failure not honored: %#v %v", res, err)
	}
	calls := f.Calls()
	if len(calls) != 2 || calls[0].Prompt != "do it" || calls[1].SessionID != "s1" {
		t.Fatalf("calls not recorded: %#v", calls)
	}
}

func TestFakeHonorsContext(t *testing.T) {
	f := NewFake()
	f.SetDelay(time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	start := time.Now()
	f.Resume(ctx, core.Session{SessionID: "s"}, "p")
	if time.Since(start) > 500*time.Millisecond {
		t.Fatal("fake must return promptly on ctx cancel")
	}
}
