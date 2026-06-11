package store

import (
	"testing"

	"github.com/tufantunc/RuntimePulse/internal/core"
)

func TestRegisterAndListSessions(t *testing.T) {
	s := openTestStore(t)
	sess, err := s.RegisterSession(core.Session{
		SessionID: "abc123", Agent: "claude", RepoPath: "/tmp/repo",
	})
	if err != nil {
		t.Fatal(err)
	}
	if sess.State != core.SessionWaiting {
		t.Fatalf("default state = %s, want waiting", sess.State)
	}
	if sess.CreatedAt.IsZero() {
		t.Fatal("CreatedAt not set")
	}

	// re-register upserts (agent change sticks, no duplicate)
	if _, err := s.RegisterSession(core.Session{
		SessionID: "abc123", Agent: "cursor", RepoPath: "/tmp/repo2",
	}); err != nil {
		t.Fatal(err)
	}
	list, err := s.ListSessions()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Agent != "cursor" || list[0].RepoPath != "/tmp/repo2" {
		t.Fatalf("upsert failed: %#v", list)
	}

	got, ok, err := s.GetSession("abc123")
	if err != nil || !ok || got.SessionID != "abc123" {
		t.Fatalf("GetSession: %#v ok=%v err=%v", got, ok, err)
	}
	if _, ok, _ := s.GetSession("missing"); ok {
		t.Fatal("GetSession should report missing")
	}

	if err := s.SetSessionState("abc123", core.SessionQueued); err != nil {
		t.Fatal(err)
	}
	got, _, _ = s.GetSession("abc123")
	if got.State != core.SessionQueued {
		t.Fatalf("state = %s, want queued", got.State)
	}

	// re-register must return DB truth: state/created_at preserved
	again, err := s.RegisterSession(core.Session{
		SessionID: "abc123", Agent: "cursor", RepoPath: "/tmp/repo3",
	})
	if err != nil {
		t.Fatal(err)
	}
	if again.State != core.SessionQueued {
		t.Fatalf("re-register returned state %s, want queued (DB truth)", again.State)
	}
	if !again.CreatedAt.Equal(got.CreatedAt) {
		t.Fatalf("re-register changed CreatedAt: %v -> %v", got.CreatedAt, again.CreatedAt)
	}
}
