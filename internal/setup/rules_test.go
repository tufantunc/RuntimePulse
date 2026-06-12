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
