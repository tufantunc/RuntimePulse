package store

import (
	"testing"
	"time"

	"github.com/tufantunc/RuntimePulse/internal/core"
)

var testPolicy = RetentionPolicy{
	SessionIdle:     7 * 24 * time.Hour,
	ContinuationAge: 30 * 24 * time.Hour,
	EventAge:        30 * 24 * time.Hour,
}

// backdateSession rewrites updated_at directly — only tests may do this.
func backdateSession(t *testing.T, s *Store, id string, at time.Time) {
	t.Helper()
	if _, err := s.db.Exec(`UPDATE sessions SET updated_at = ? WHERE session_id = ?`, ts(at), id); err != nil {
		t.Fatal(err)
	}
}

func backdateContinuation(t *testing.T, s *Store, id string, at time.Time) {
	t.Helper()
	if _, err := s.db.Exec(`UPDATE continuations SET updated_at = ? WHERE id = ?`, ts(at), id); err != nil {
		t.Fatal(err)
	}
}

func registerTestSession(t *testing.T, s *Store, id string) {
	t.Helper()
	if _, err := s.RegisterSession(core.Session{SessionID: id, Agent: "claude", RepoPath: "/tmp"}); err != nil {
		t.Fatal(err)
	}
}

func sessionExists(t *testing.T, s *Store, id string) bool {
	t.Helper()
	_, ok, err := s.GetSession(id)
	if err != nil {
		t.Fatal(err)
	}
	return ok
}

func TestSweepSessionMatrix(t *testing.T) {
	s := openTestStore(t)
	now := time.Now().UTC()
	old := now.Add(-8 * 24 * time.Hour) // past the 7d window

	// 1) old + no rule + waiting → DELETED
	registerTestSession(t, s, "old-bare")
	backdateSession(t, s, "old-bare", old)

	// 2) old + ACTIVE rule → KEPT
	registerTestSession(t, s, "old-armed")
	mustAddRule(t, s, core.Rule{
		Selector: core.EventSelector{Type: "tcp.available"}, SessionID: "old-armed", PromptTemplate: "go",
	})
	backdateSession(t, s, "old-armed", old)

	// 3) old + RUNNING → KEPT
	registerTestSession(t, s, "old-running")
	if err := s.SetSessionState("old-running", core.SessionRunning); err != nil {
		t.Fatal(err)
	}
	backdateSession(t, s, "old-running", old)

	// 4) young + no rule → KEPT (age gate)
	registerTestSession(t, s, "young-bare")

	// 5) old + EXPIRED rule only → DELETED (expired rule is not active)
	registerTestSession(t, s, "old-expired-rule")
	past := now.Add(-time.Hour)
	expired := core.Rule{
		Selector: core.EventSelector{Type: "tcp.available"}, SessionID: "old-expired-rule", PromptTemplate: "go",
	}
	expired.ExpiresAt = &past
	mustAddRule(t, s, expired)
	backdateSession(t, s, "old-expired-rule", old)

	// 6) old + CONSUMED oneShot rule only → DELETED
	registerTestSession(t, s, "old-consumed-rule")
	mustAddRule(t, s, core.Rule{
		Selector:  core.EventSelector{Type: "exec.succeeded", Source: "gc-test"},
		SessionID: "old-consumed-rule", PromptTemplate: "go", OneShot: true,
	})
	if _, err := s.Ingest(core.Event{ID: "evt-gc-1", Type: "exec.succeeded", Source: "gc-test", Timestamp: now}); err != nil {
		t.Fatal(err) // consumes the oneShot rule
	}
	backdateSession(t, s, "old-consumed-rule", old)

	res, err := s.Sweep(now, testPolicy)
	if err != nil {
		t.Fatal(err)
	}
	if res.Sessions != 3 {
		t.Fatalf("swept %d sessions, want 3 (old-bare, old-expired-rule, old-consumed-rule)", res.Sessions)
	}
	for id, want := range map[string]bool{
		"old-bare": false, "old-armed": true, "old-running": true,
		"young-bare": true, "old-expired-rule": false, "old-consumed-rule": false,
	} {
		if got := sessionExists(t, s, id); got != want {
			t.Errorf("session %s: exists=%v want %v", id, got, want)
		}
	}
}

func TestSweepContinuations(t *testing.T) {
	s := openTestStore(t)
	now := time.Now().UTC()
	old := now.Add(-31 * 24 * time.Hour)

	c1 := seedContinuation(t, s, "sess-a", "evt-a") // pending
	// terminal + old → deleted
	c2 := seedContinuationFor(t, s, "sess-b", "evt-b", "http.available")
	s.ClaimContinuation(c2.ID)
	exit := 0
	s.UpdateContinuationResult(c2.ID, core.ContinuationCompleted, "cmd", &exit, "done")
	backdateContinuation(t, s, c2.ID, old)
	// pending + old → KEPT (never touch non-terminal)
	backdateContinuation(t, s, c1.ID, old)
	// terminal + young → kept
	c3 := seedContinuationFor(t, s, "sess-c", "evt-c", "process.started")
	s.ClaimContinuation(c3.ID)
	s.UpdateContinuationResult(c3.ID, core.ContinuationFailed, "cmd", &exit, "boom")

	res, err := s.Sweep(now, testPolicy)
	if err != nil {
		t.Fatal(err)
	}
	if res.Continuations != 1 {
		t.Fatalf("swept %d continuations, want 1", res.Continuations)
	}
	all, _ := s.ListContinuations("")
	ids := map[string]bool{}
	for _, c := range all {
		ids[c.ID] = true
	}
	if !ids[c1.ID] || ids[c2.ID] || !ids[c3.ID] {
		t.Fatalf("wrong survivors: %v", ids)
	}
}

// seedContinuationFor mirrors seedContinuation with a custom event type
// so multiple seeds in one test don't collide on rules/events.
func seedContinuationFor(t *testing.T, s *Store, sessionID, evtID, evType string) core.Continuation {
	t.Helper()
	mustAddRule(t, s, core.Rule{
		Selector: core.EventSelector{Type: evType}, SessionID: sessionID,
		PromptTemplate: "go", Label: "seed-" + sessionID,
	})
	res, err := s.Ingest(core.Event{ID: evtID, Type: evType, Source: "x", Timestamp: time.Now().UTC()})
	if err != nil || len(res.Continuations) != 1 {
		t.Fatalf("seed failed: %v %d", err, len(res.Continuations))
	}
	return res.Continuations[0]
}

func TestSweepEvents(t *testing.T) {
	s := openTestStore(t)
	now := time.Now().UTC()
	if err := s.InsertEvent(core.Event{ID: "evt-old", Type: "x", Source: "s", Timestamp: now.Add(-31 * 24 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if err := s.InsertEvent(core.Event{ID: "evt-new", Type: "x", Source: "s", Timestamp: now}); err != nil {
		t.Fatal(err)
	}
	res, err := s.Sweep(now, testPolicy)
	if err != nil {
		t.Fatal(err)
	}
	if res.Events != 1 {
		t.Fatalf("swept %d events, want 1", res.Events)
	}
}

func TestSweepIsIdempotent(t *testing.T) {
	s := openTestStore(t)
	now := time.Now().UTC()
	registerTestSession(t, s, "old-bare")
	backdateSession(t, s, "old-bare", now.Add(-8*24*time.Hour))
	if _, err := s.Sweep(now, testPolicy); err != nil {
		t.Fatal(err)
	}
	res, err := s.Sweep(now, testPolicy)
	if err != nil {
		t.Fatal(err)
	}
	if res.Sessions+res.Continuations+res.Events != 0 {
		t.Fatalf("second sweep must delete nothing: %+v", res)
	}
}
