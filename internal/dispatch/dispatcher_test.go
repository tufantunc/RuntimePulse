package dispatch

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/tufantunc/RuntimePulse/internal/adapter"
	"github.com/tufantunc/RuntimePulse/internal/bus"
	"github.com/tufantunc/RuntimePulse/internal/core"
	"github.com/tufantunc/RuntimePulse/internal/engine"
	"github.com/tufantunc/RuntimePulse/internal/store"
)

type fixture struct {
	store *store.Store
	eng   *engine.Engine
	fake  *adapter.Fake
	disp  *Dispatcher
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	b := bus.New()
	eng := engine.New(st, b)
	fake := adapter.NewFake()
	d := New(st, adapter.Registry{"fake": fake}, eng.Ingest)
	eng.Notify = d.Wake

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go d.Run(ctx)
	return &fixture{store: st, eng: eng, fake: fake, disp: d}
}

func (f *fixture) registerSession(t *testing.T, id string) {
	t.Helper()
	if _, err := f.store.RegisterSession(core.Session{SessionID: id, Agent: "fake", RepoPath: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
}

func (f *fixture) addRule(t *testing.T, evType, sessionID, label string) {
	t.Helper()
	if _, err := f.store.AddRule(core.Rule{
		Selector: core.EventSelector{Type: evType}, SessionID: sessionID,
		PromptTemplate: "continue " + sessionID, Label: label, OneShot: true,
	}); err != nil {
		t.Fatal(err)
	}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestDispatchCompletesAndEmitsResultEvent(t *testing.T) {
	f := newFixture(t)
	f.registerSession(t, "sess-1")
	f.addRule(t, "tcp.available", "sess-1", "step-1")

	if _, err := f.eng.Ingest(core.Event{Type: "tcp.available", Source: "x"}); err != nil {
		t.Fatal(err)
	}

	waitFor(t, "continuation completed", func() bool {
		done, _ := f.store.ListContinuations("completed")
		return len(done) == 1
	})
	if calls := f.fake.Calls(); len(calls) != 1 || calls[0].Prompt != "continue sess-1" {
		t.Fatalf("adapter not called with stored prompt: %#v", calls)
	}
	// result event with the rule label as source (spec §4.5)
	waitFor(t, "continuation.completed event", func() bool {
		evs, _ := f.store.ListEvents("continuation.completed", 10)
		return len(evs) == 1 && evs[0].Source == "step-1"
	})
}

func TestDispatchFailureEmitsFailedEvent(t *testing.T) {
	f := newFixture(t)
	f.registerSession(t, "sess-1")
	f.addRule(t, "tcp.available", "sess-1", "")
	f.fake.SetOutcome(2, nil) // agent ran, exited 2

	f.eng.Ingest(core.Event{Type: "tcp.available", Source: "x"})
	waitFor(t, "failed continuation", func() bool {
		failed, _ := f.store.ListContinuations("failed")
		return len(failed) == 1
	})
	waitFor(t, "continuation.failed event", func() bool {
		evs, _ := f.store.ListEvents("continuation.failed", 10)
		return len(evs) == 1
	})
}

func TestDispatchChaining(t *testing.T) {
	f := newFixture(t)
	f.registerSession(t, "sess-1")
	f.addRule(t, "tcp.available", "sess-1", "step-1")
	// step-2 fires on step-1's completion — pure rule mechanics
	if _, err := f.store.AddRule(core.Rule{
		Selector:  core.EventSelector{Type: "continuation.completed", Source: "step-1"},
		SessionID: "sess-1", PromptTemplate: "step 2", Label: "step-2", OneShot: true,
	}); err != nil {
		t.Fatal(err)
	}

	f.eng.Ingest(core.Event{Type: "tcp.available", Source: "x"})
	waitFor(t, "two completed continuations (chain)", func() bool {
		done, _ := f.store.ListContinuations("completed")
		return len(done) == 2
	})
	calls := f.fake.Calls()
	if len(calls) != 2 || calls[1].Prompt != "step 2" {
		t.Fatalf("chain did not run step 2: %#v", calls)
	}
}

func TestDispatchFIFOWithinSession(t *testing.T) {
	f := newFixture(t)
	f.registerSession(t, "sess-1")
	f.fake.SetDelay(50 * time.Millisecond)
	// two non-oneShot rules on the same event type, same session
	for _, label := range []string{"a", "b"} {
		if _, err := f.store.AddRule(core.Rule{
			Selector: core.EventSelector{Type: "tcp.available"}, SessionID: "sess-1",
			PromptTemplate: "p-" + label, Label: label,
		}); err != nil {
			t.Fatal(err)
		}
	}
	f.eng.Ingest(core.Event{Type: "tcp.available", Source: "x"})
	waitFor(t, "both continuations done", func() bool {
		done, _ := f.store.ListContinuations("completed")
		return len(done) == 2
	})
	calls := f.fake.Calls()
	if len(calls) != 2 {
		t.Fatalf("want 2 serial calls, got %d", len(calls))
	}
}

func TestDispatchUnknownAgentFails(t *testing.T) {
	f := newFixture(t)
	if _, err := f.store.RegisterSession(core.Session{SessionID: "sess-x", Agent: "cursor", RepoPath: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	f.addRule(t, "tcp.available", "sess-x", "")
	f.eng.Ingest(core.Event{Type: "tcp.available", Source: "x"})
	waitFor(t, "unknown-agent failure", func() bool {
		failed, _ := f.store.ListContinuations("failed")
		return len(failed) == 1 && failed[0].OutputSummary != ""
	})
}

func TestShutdownLeavesInFlightRunning(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	b := bus.New()
	eng := engine.New(st, b)
	fake := adapter.NewFake()
	fake.SetDelay(10 * time.Second) // long-running agent turn
	d := New(st, adapter.Registry{"fake": fake}, eng.Ingest)
	eng.Notify = d.Wake

	if _, err := st.RegisterSession(core.Session{SessionID: "sess-1", Agent: "fake", RepoPath: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddRule(core.Rule{
		Selector: core.EventSelector{Type: "tcp.available"}, SessionID: "sess-1", PromptTemplate: "go",
	}); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { d.Run(ctx); close(done) }()

	eng.Ingest(core.Event{Type: "tcp.available", Source: "x"})
	waitFor(t, "continuation claimed", func() bool {
		running, _ := st.ListContinuations("running")
		return len(running) == 1
	})

	cancel() // graceful shutdown mid-run
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not drain workers on shutdown")
	}

	running, _ := st.ListContinuations("running")
	failed, _ := st.ListContinuations("failed")
	if len(running) != 1 || len(failed) != 0 {
		t.Fatalf("shutdown must leave in-flight work running for recovery: running=%d failed=%d",
			len(running), len(failed))
	}
	evs, _ := st.ListEvents("continuation.failed", 10)
	if len(evs) != 0 {
		t.Fatal("shutdown must not emit continuation.failed events")
	}
}

func TestBootRecovery(t *testing.T) {
	// store with an orphaned running continuation, dispatcher started after
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	b := bus.New()
	eng := engine.New(st, b)
	if _, err := st.RegisterSession(core.Session{SessionID: "sess-1", Agent: "fake", RepoPath: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddRule(core.Rule{
		Selector: core.EventSelector{Type: "tcp.available"}, SessionID: "sess-1", PromptTemplate: "go",
	}); err != nil {
		t.Fatal(err)
	}
	res, err := st.Ingest(core.Event{ID: "evt-1", Type: "tcp.available", Source: "x", Timestamp: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	st.ClaimContinuation(res.Continuations[0].ID) // simulate crash mid-run

	fake := adapter.NewFake()
	d := New(st, adapter.Registry{"fake": fake}, eng.Ingest)
	eng.Notify = d.Wake
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go d.Run(ctx)

	waitFor(t, "recovered continuation", func() bool {
		done, _ := st.ListContinuations("completed")
		return len(done) == 1
	})
}
