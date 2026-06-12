package daemon

import (
	"testing"

	"github.com/tufantunc/RuntimePulse/internal/core"
)

// The time-window matrix lives in the store tests (backdating rows
// requires in-package db access). Here we only prove the wiring: the
// daemon's sweep runs against the real policy and never touches young
// state.
func TestSweepOnceKeepsYoungState(t *testing.T) {
	d, _ := startTestDaemon(t)
	if _, err := d.Engine.Store.RegisterSession(core.Session{
		SessionID: "fresh", Agent: "claude", RepoPath: t.TempDir(),
	}); err != nil {
		t.Fatal(err)
	}
	d.sweepOnce()
	if _, ok, err := d.Engine.Store.GetSession("fresh"); err != nil || !ok {
		t.Fatalf("young session must survive a sweep: ok=%v err=%v", ok, err)
	}
}
