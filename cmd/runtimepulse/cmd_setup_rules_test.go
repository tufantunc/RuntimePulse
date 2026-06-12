package main

import (
	"strings"
	"testing"

	"github.com/tufantunc/RuntimePulse/internal/setup"
)

func TestShouldWriteRules(t *testing.T) {
	cases := []struct {
		name           string
		rules, noRules bool
		interactive    bool
		promptYes      bool
		want           bool
	}{
		{"explicit --no-rules wins", true, true, true, true, false},
		{"explicit --rules", true, false, false, false, true},
		{"neither, non-interactive", false, false, false, false, false},
		{"neither, interactive yes", false, false, true, true, true},
		{"neither, interactive no", false, false, true, false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := shouldWriteRules(c.rules, c.noRules, c.interactive, func() bool { return c.promptYes })
			if got != c.want {
				t.Fatalf("got %v, want %v", got, c.want)
			}
		})
	}
}

func TestFormatRuleResults(t *testing.T) {
	out := formatRuleResults([]setup.RuleResult{
		{Agent: "claude", Action: "written", Path: "/h/.claude/CLAUDE.md"},
		{Agent: "opencode", Action: "unchanged", Path: "/h/.config/opencode/AGENTS.md"},
		{Agent: "cursor", Action: "manual", Manual: true, Text: "PASTE-BODY"},
	})
	if !strings.Contains(out, "claude") || !strings.Contains(out, "rules written") {
		t.Fatalf("missing written line:\n%s", out)
	}
	if !strings.Contains(out, "rules unchanged") {
		t.Fatalf("missing unchanged line:\n%s", out)
	}
	if !strings.Contains(out, "paste manually") || !strings.Contains(out, "PASTE-BODY") {
		t.Fatalf("missing cursor paste block:\n%s", out)
	}
}
