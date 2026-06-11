package store

import (
	"testing"

	"github.com/tufantunc/RuntimePulse/internal/core"
)

// seedContinuation ingests one event against one rule and returns the continuation.
func seedContinuation(t *testing.T, s *Store, sessionID, evtID string) core.Continuation {
	t.Helper()
	mustAddRule(t, s, core.Rule{
		Selector: core.EventSelector{Type: "tcp.available"}, SessionID: sessionID,
		PromptTemplate: "go", Label: "seed",
	})
	res, err := s.Ingest(core.Event{ID: evtID, Type: "tcp.available", Source: "x"})
	if err != nil || len(res.Continuations) != 1 {
		t.Fatalf("seed failed: %v %d", err, len(res.Continuations))
	}
	return res.Continuations[0]
}

func TestClaimContinuation(t *testing.T) {
	s := openTestStore(t)
	c := seedContinuation(t, s, "sess-1", "evt-1")

	ok, err := s.ClaimContinuation(c.ID)
	if err != nil || !ok {
		t.Fatalf("first claim must succeed: ok=%v err=%v", ok, err)
	}
	ok, err = s.ClaimContinuation(c.ID)
	if err != nil || ok {
		t.Fatalf("second claim must be rejected: ok=%v err=%v", ok, err)
	}
	running, _ := s.ListContinuations("running")
	if len(running) != 1 {
		t.Fatalf("claimed continuation must be running: %d", len(running))
	}
}

func TestUpdateContinuationResult(t *testing.T) {
	s := openTestStore(t)
	c := seedContinuation(t, s, "sess-1", "evt-1")
	s.ClaimContinuation(c.ID)

	exit := 0
	if err := s.UpdateContinuationResult(c.ID, core.ContinuationCompleted,
		"claude --resume sess-1 ...", &exit, "did the thing"); err != nil {
		t.Fatal(err)
	}
	done, _ := s.ListContinuations("completed")
	if len(done) != 1 || done[0].Command == "" || done[0].ExitCode == nil || *done[0].ExitCode != 0 ||
		done[0].OutputSummary != "did the thing" {
		t.Fatalf("result not recorded: %#v", done)
	}
}

func TestResetRunningContinuations(t *testing.T) {
	s := openTestStore(t)
	c := seedContinuation(t, s, "sess-1", "evt-1")
	s.ClaimContinuation(c.ID)

	n, err := s.ResetRunningContinuations()
	if err != nil || n != 1 {
		t.Fatalf("reset: n=%d err=%v", n, err)
	}
	pending, _ := s.ListContinuations("pending")
	if len(pending) != 1 {
		t.Fatal("running continuation must be back to pending after reset")
	}
}

func TestNextPendingAndPendingSessions(t *testing.T) {
	s := openTestStore(t)
	c1 := seedContinuation(t, s, "sess-1", "evt-1")
	// second rule+event for another session
	mustAddRule(t, s, core.Rule{
		Selector: core.EventSelector{Type: "http.available"}, SessionID: "sess-2", PromptTemplate: "go",
	})
	if _, err := s.Ingest(core.Event{ID: "evt-2", Type: "http.available", Source: "y"}); err != nil {
		t.Fatal(err)
	}

	sessions, err := s.PendingSessions()
	if err != nil || len(sessions) != 2 {
		t.Fatalf("PendingSessions = %v err=%v, want 2", sessions, err)
	}

	next, ok, err := s.NextPendingForSession("sess-1")
	if err != nil || !ok || next.ID != c1.ID {
		t.Fatalf("NextPendingForSession: %#v ok=%v err=%v", next, ok, err)
	}
	if _, ok, _ := s.NextPendingForSession("sess-none"); ok {
		t.Fatal("no pending for unknown session")
	}
}
