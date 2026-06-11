package store

import (
	"testing"
	"time"

	"github.com/tufantunc/RuntimePulse/internal/core"
)

func testRule(sel core.EventSelector) core.Rule {
	return core.Rule{
		Selector:       sel,
		ActionKind:     core.ActionContinueSession,
		SessionID:      "abc123",
		PromptTemplate: "go on",
	}
}

func TestAddListRemoveRules(t *testing.T) {
	s := openTestStore(t)
	r, err := s.AddRule(testRule(core.EventSelector{Type: "docker.healthy", Source: "postgres"}))
	if err != nil {
		t.Fatal(err)
	}
	if r.ID == "" || r.CreatedAt.IsZero() {
		t.Fatalf("AddRule must fill id and created_at: %#v", r)
	}

	past := time.Now().UTC().Add(-time.Hour)
	expired := testRule(core.EventSelector{Type: "docker.healthy"})
	expired.ExpiresAt = &past
	if _, err := s.AddRule(expired); err != nil {
		t.Fatal(err)
	}

	active, err := s.ListRules(false)
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 1 || active[0].ID != r.ID {
		t.Fatalf("ListRules(false) must exclude expired: %#v", active)
	}
	all, err := s.ListRules(true)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("ListRules(true) = %d, want 2", len(all))
	}

	if err := s.RemoveRule(r.ID); err != nil {
		t.Fatal(err)
	}
	active, _ = s.ListRules(false)
	if len(active) != 0 {
		t.Fatalf("rule not removed: %#v", active)
	}
}
