package setup

import (
	"os"
	"path/filepath"
	"strings"
)

// releaseResumeRuleBody is the single source of truth for the guidance,
// mirroring the server-level MCP Instructions in internal/mcpserver/server.go.
const releaseResumeRuleBody = `## RuntimePulse: release-and-resume

When you need to wait for a runtime condition (a service becoming healthy, a
build finishing, a file appearing), do NOT poll, sleep, or block. Instead:

1. Call the ` + "`create_rule`" + ` MCP tool with your own session id (pass agent +
   repoPath to self-register; use oneShot:true for wake-me-once).
2. If a watcher is needed, call ` + "`create_watch`" + ` AFTER the rule (a new watch
   emits current state immediately, so the rule must already exist).
3. End your turn. RuntimePulse resumes your session with the event as a new
   prompt when the condition is met.

Use ` + "`wait_for_event`" + ` only for conditions expected within seconds.`

const (
	ruleBeginMarker = "<!-- runtimepulse:begin (managed — do not edit inside) -->"
	ruleEndMarker   = "<!-- runtimepulse:end -->"
)

// managedBlock is the canonical marker-delimited block (no trailing newline).
func managedBlock() string {
	return ruleBeginMarker + "\n" + strings.TrimSpace(releaseResumeRuleBody) + "\n" + ruleEndMarker
}

// mergeMarkerBlock ensures the managed rule block is present in the file at
// path, writing atomically. Returns the action: "written" (new file),
// "updated" (stale block replaced), "appended" (added to existing content),
// or "unchanged". With dryRun it writes nothing and returns "would-write"
// when a write would occur, "unchanged" otherwise.
func mergeMarkerBlock(path string, dryRun bool) (string, error) {
	block := managedBlock()
	raw, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}

	var out, action string
	switch {
	case os.IsNotExist(err):
		out, action = block+"\n", "written"
	default:
		content := string(raw)
		b := strings.Index(content, ruleBeginMarker)
		e := strings.Index(content, ruleEndMarker)
		if b >= 0 && e >= b {
			rebuilt := content[:b] + block + content[e+len(ruleEndMarker):]
			if rebuilt == content {
				return "unchanged", nil
			}
			out, action = rebuilt, "updated"
		} else {
			out = strings.TrimRight(content, "\n") + "\n\n" + block + "\n"
			action = "appended"
		}
	}

	if dryRun {
		return "would-write", nil
	}
	if err := atomicWrite(path, []byte(out)); err != nil {
		return "", err
	}
	return action, nil
}

// RuleResult is the outcome of installing the rule for one agent.
type RuleResult struct {
	Agent  string
	Action string // written | updated | appended | unchanged | would-write | manual
	Path   string // file written, or "" for manual
	Manual bool   // true for cursor — caller prints Text
	Text   string // marker-less paste text (manual only)
	Err    error
}

// RuleWriter installs the global release-and-resume rule for one agent.
type RuleWriter interface {
	WriteRule(dryRun bool) RuleResult
}

// fileRuleWriter writes the marker block into a global instruction file.
type fileRuleWriter struct {
	agent string
	path  string
}

func (w fileRuleWriter) WriteRule(dryRun bool) RuleResult {
	action, err := mergeMarkerBlock(w.path, dryRun)
	return RuleResult{Agent: w.agent, Action: action, Path: w.path, Err: err}
}

// manualRuleWriter writes nothing; it returns paste-ready text (cursor).
type manualRuleWriter struct{ agent string }

func (w manualRuleWriter) WriteRule(dryRun bool) RuleResult {
	return RuleResult{
		Agent:  w.agent,
		Action: "manual",
		Manual: true,
		Text:   strings.TrimSpace(releaseResumeRuleBody),
	}
}

// ruleWriterFor returns the writer for an agent, or nil if unknown.
// File paths are home-based to match the package's testable agents.
func ruleWriterFor(name, home string) RuleWriter {
	switch name {
	case "claude":
		return fileRuleWriter{agent: "claude", path: filepath.Join(home, ".claude", "CLAUDE.md")}
	case "codex":
		return fileRuleWriter{agent: "codex", path: filepath.Join(home, ".codex", "AGENTS.md")}
	case "opencode":
		return fileRuleWriter{agent: "opencode", path: filepath.Join(home, ".config", "opencode", "AGENTS.md")}
	case "cursor":
		return manualRuleWriter{agent: "cursor"}
	}
	return nil
}

// WriteRules installs the rule for each selected agent, preserving the
// order of agents. Mirrors Run; rule install is always global scope.
func WriteRules(agents []Agent, names []string, home string, dryRun bool) []RuleResult {
	selected := map[string]bool{}
	for _, n := range names {
		selected[n] = true
	}
	var out []RuleResult
	for _, a := range agents {
		if !selected[a.Name()] {
			continue
		}
		if w := ruleWriterFor(a.Name(), home); w != nil {
			out = append(out, w.WriteRule(dryRun))
		}
	}
	return out
}
