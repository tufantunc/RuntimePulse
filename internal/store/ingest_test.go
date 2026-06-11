package store

import (
	"strings"
	"testing"
	"time"

	"github.com/tufantunc/RuntimePulse/internal/core"
)

func mustAddRule(t *testing.T, s *Store, r core.Rule) core.Rule {
	t.Helper()
	added, err := s.AddRule(r)
	if err != nil {
		t.Fatal(err)
	}
	return added
}

func ingestEvent(t *testing.T, s *Store, id string) IngestResult {
	t.Helper()
	res, err := s.Ingest(core.Event{
		ID: id, Type: "docker.healthy", Source: "postgres",
		Payload: map[string]string{"container": "postgres"}, Timestamp: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestIngestMatchesAndCreatesContinuation(t *testing.T) {
	s := openTestStore(t)
	r := mustAddRule(t, s, core.Rule{
		Selector:       core.EventSelector{Type: "docker.healthy", Source: "postgres"},
		SessionID:      "abc123",
		PromptTemplate: "{{.Event.Source}} healthy. Continue.",
		OneShot:        true,
	})
	// non-matching rule must not fire
	mustAddRule(t, s, core.Rule{
		Selector: core.EventSelector{Type: "http.available"}, SessionID: "abc123", PromptTemplate: "x",
	})

	res := ingestEvent(t, s, "evt-1")
	if len(res.Continuations) != 1 {
		t.Fatalf("got %d continuations, want 1", len(res.Continuations))
	}
	c := res.Continuations[0]
	if c.Prompt != "postgres healthy. Continue." {
		t.Fatalf("prompt = %q", c.Prompt)
	}
	if c.State != core.ContinuationPending || c.RuleID != r.ID || c.EventID != "evt-1" {
		t.Fatalf("bad continuation: %#v", c)
	}

	// oneShot rule is consumed in the same transaction
	active, _ := s.ListRules(false)
	for _, a := range active {
		if a.ID == r.ID {
			t.Fatal("oneShot rule should be consumed")
		}
	}
}

func TestIngestIsIdempotent(t *testing.T) {
	s := openTestStore(t)
	mustAddRule(t, s, core.Rule{
		Selector: core.EventSelector{Type: "docker.healthy"}, SessionID: "abc123", PromptTemplate: "go",
	})
	first := ingestEvent(t, s, "evt-1")
	second := ingestEvent(t, s, "evt-1") // same event re-delivered
	if len(first.Continuations) != 1 || len(second.Continuations) != 0 {
		t.Fatalf("idempotency broken: first=%d second=%d",
			len(first.Continuations), len(second.Continuations))
	}
	all, err := s.ListContinuations("")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 {
		t.Fatalf("store has %d continuations, want 1", len(all))
	}
}

func TestIngestSkipsExpiredRules(t *testing.T) {
	s := openTestStore(t)
	past := time.Now().UTC().Add(-time.Minute)
	r := core.Rule{
		Selector: core.EventSelector{Type: "docker.healthy"}, SessionID: "abc123", PromptTemplate: "go",
	}
	r.ExpiresAt = &past
	mustAddRule(t, s, r)
	if res := ingestEvent(t, s, "evt-1"); len(res.Continuations) != 0 {
		t.Fatal("expired rule must not fire")
	}
}

func TestIngestRenderErrorYieldsFailedContinuation(t *testing.T) {
	s := openTestStore(t)
	mustAddRule(t, s, core.Rule{
		Selector: core.EventSelector{Type: "docker.healthy"}, SessionID: "abc123",
		PromptTemplate: "{{.Event.Bogus}}",
	})
	res := ingestEvent(t, s, "evt-1")
	if len(res.Continuations) != 1 {
		t.Fatalf("got %d, want 1 failed continuation", len(res.Continuations))
	}
	c := res.Continuations[0]
	if c.State != core.ContinuationFailed || !strings.Contains(c.OutputSummary, "Bogus") {
		t.Fatalf("render error not surfaced: %#v", c)
	}
}
