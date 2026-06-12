package workflow

import (
	"strings"
	"testing"
)

const validYAML = `
session: abc123
agent: claude
repo: /tmp/x
steps:
  - label: step-1
    on: { type: docker.healthy, source: postgres }
    prompt: "Postgres is ready. Run the migrations."
  - on: { type: continuation.completed, source: step-1 }
    prompt: "Migrations done. Run the tests."
`

func TestParseAndCompile(t *testing.T) {
	w, err := Parse([]byte(validYAML))
	if err != nil {
		t.Fatal(err)
	}
	if w.Session != "abc123" || w.Agent != "claude" || w.Repo != "/tmp/x" {
		t.Fatalf("header lost: %#v", w)
	}
	rules := w.Rules()
	if len(rules) != 2 {
		t.Fatalf("got %d rules", len(rules))
	}
	if rules[0].Label != "step-1" || rules[1].Label != "step-2" {
		t.Fatalf("labels: %q %q (second must default to step-N)", rules[0].Label, rules[1].Label)
	}
	if !rules[0].OneShot || !rules[1].OneShot {
		t.Fatal("workflow rules must be oneShot")
	}
	if rules[1].Selector.Type != "continuation.completed" || rules[1].Selector.Source != "step-1" {
		t.Fatalf("chain selector lost: %#v", rules[1].Selector)
	}
	if rules[0].SessionID != "abc123" {
		t.Fatalf("session not propagated: %#v", rules[0])
	}
}

func TestParseRejectsInvalid(t *testing.T) {
	cases := []struct{ name, yaml, wantErr string }{
		{"missing session", "agent: claude\nsteps:\n  - on: {type: x}\n    prompt: p\n", "session"},
		{"missing agent", "session: s\nsteps:\n  - on: {type: x}\n    prompt: p\n", "agent"},
		{"no steps", "session: s\nagent: claude\n", "step"},
		{"missing type", "session: s\nagent: claude\nsteps:\n  - prompt: p\n", "type"},
		{"missing prompt", "session: s\nagent: claude\nsteps:\n  - on: {type: x}\n", "prompt"},
		{"duplicate labels", "session: s\nagent: claude\nsteps:\n  - {label: a, on: {type: x}, prompt: p}\n  - {label: a, on: {type: y}, prompt: p}\n", "duplicate"},
		{"bad template", "session: s\nagent: claude\nsteps:\n  - on: {type: x}\n    prompt: \"{{.Event.Bad\"\n", "template"},
		{"unknown field", "session: s\nagent: claude\nsessoin: typo\nsteps:\n  - on: {type: x}\n    prompt: p\n", "field"},
	}
	for _, c := range cases {
		if _, err := Parse([]byte(c.yaml)); err == nil || !strings.Contains(strings.ToLower(err.Error()), c.wantErr) {
			t.Errorf("%s: err = %v, want mention of %q", c.name, err, c.wantErr)
		}
	}
}
