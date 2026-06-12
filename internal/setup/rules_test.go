package setup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMergeMarkerBlockNewFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "CLAUDE.md")
	action, err := mergeMarkerBlock(path, false)
	if err != nil {
		t.Fatal(err)
	}
	if action != "written" {
		t.Fatalf("action = %q, want written", action)
	}
	b, _ := os.ReadFile(path)
	got := string(b)
	if !strings.Contains(got, ruleBeginMarker) || !strings.Contains(got, ruleEndMarker) {
		t.Fatalf("markers missing:\n%s", got)
	}
	if !strings.Contains(got, "release-and-resume") {
		t.Fatalf("rule body missing:\n%s", got)
	}
}

func TestMergeMarkerBlockAppendsPreservingContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "AGENTS.md")
	original := "# My rules\n\nUse tabs, not spaces.\n"
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	action, err := mergeMarkerBlock(path, false)
	if err != nil {
		t.Fatal(err)
	}
	if action != "appended" {
		t.Fatalf("action = %q, want appended", action)
	}
	b, _ := os.ReadFile(path)
	got := string(b)
	if !strings.HasPrefix(got, original) {
		t.Fatalf("existing content not preserved verbatim at start:\n%s", got)
	}
	if !strings.Contains(got, ruleBeginMarker) {
		t.Fatalf("block not appended:\n%s", got)
	}
}

func TestMergeMarkerBlockReplacesInPlace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "AGENTS.md")
	stale := "# top\n\n" + ruleBeginMarker + "\nOLD STALE TEXT\n" + ruleEndMarker + "\n\n# bottom\n"
	if err := os.WriteFile(path, []byte(stale), 0o600); err != nil {
		t.Fatal(err)
	}
	action, err := mergeMarkerBlock(path, false)
	if err != nil {
		t.Fatal(err)
	}
	if action != "updated" {
		t.Fatalf("action = %q, want updated", action)
	}
	b, _ := os.ReadFile(path)
	got := string(b)
	if strings.Contains(got, "OLD STALE TEXT") {
		t.Fatalf("stale block not replaced:\n%s", got)
	}
	if !strings.Contains(got, "# top") || !strings.Contains(got, "# bottom") {
		t.Fatalf("surrounding content lost:\n%s", got)
	}
	if strings.Count(got, ruleBeginMarker) != 1 {
		t.Fatalf("expected exactly one block, got %d:\n%s", strings.Count(got, ruleBeginMarker), got)
	}
}

func TestMergeMarkerBlockIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "CLAUDE.md")
	if _, err := mergeMarkerBlock(path, false); err != nil {
		t.Fatal(err)
	}
	action, err := mergeMarkerBlock(path, false)
	if err != nil {
		t.Fatal(err)
	}
	if action != "unchanged" {
		t.Fatalf("second run action = %q, want unchanged", action)
	}
}

func TestMergeMarkerBlockDryRunWritesNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "CLAUDE.md")
	action, err := mergeMarkerBlock(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if action != "would-write" {
		t.Fatalf("action = %q, want would-write", action)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("dry-run created the file")
	}
}

func TestFileRuleWriterWritesGlobalFile(t *testing.T) {
	home := t.TempDir()
	w := ruleWriterFor("claude", home)
	if w == nil {
		t.Fatal("no writer for claude")
	}
	res := w.WriteRule(false)
	if res.Err != nil {
		t.Fatal(res.Err)
	}
	if res.Action != "written" {
		t.Fatalf("action = %q, want written", res.Action)
	}
	want := filepath.Join(home, ".claude", "CLAUDE.md")
	if res.Path != want {
		t.Fatalf("path = %q, want %q", res.Path, want)
	}
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("file not written: %v", err)
	}
}

func TestRuleWriterForPaths(t *testing.T) {
	home := "/home/u"
	cases := map[string]string{
		"claude":   filepath.Join(home, ".claude", "CLAUDE.md"),
		"codex":    filepath.Join(home, ".codex", "AGENTS.md"),
		"opencode": filepath.Join(home, ".config", "opencode", "AGENTS.md"),
	}
	for name, wantPath := range cases {
		w := ruleWriterFor(name, home)
		fw, ok := w.(fileRuleWriter)
		if !ok {
			t.Fatalf("%s: not a fileRuleWriter", name)
		}
		if fw.path != wantPath {
			t.Fatalf("%s path = %q, want %q", name, fw.path, wantPath)
		}
	}
}

func TestManualRuleWriterCursor(t *testing.T) {
	w := ruleWriterFor("cursor", t.TempDir())
	res := w.WriteRule(false)
	if !res.Manual || res.Action != "manual" {
		t.Fatalf("cursor should be manual: %#v", res)
	}
	if res.Path != "" {
		t.Fatalf("cursor should write no file, got path %q", res.Path)
	}
	if !strings.Contains(res.Text, "release-and-resume") {
		t.Fatalf("paste text missing rule body: %q", res.Text)
	}
	if strings.Contains(res.Text, ruleBeginMarker) {
		t.Fatalf("paste text must not contain markers: %q", res.Text)
	}
}

func TestWriteRulesSelectsInstalledAgents(t *testing.T) {
	home := t.TempDir()
	agents := []Agent{claudeAgent{}, cursorAgent{home: home}, codexAgent{}, openCodeAgent{home: home}}
	results := WriteRules(agents, []string{"claude", "cursor"}, home, false)
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
	if results[0].Agent != "claude" || results[1].Agent != "cursor" {
		t.Fatalf("unexpected order/agents: %#v", results)
	}
}
