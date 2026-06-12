// Package workflow compiles declarative workflow files into rules.
// There is no workflow entity in the core (spec §7.3): a workflow is
// syntax sugar that emits oneShot rules, chained through
// continuation.completed events.
package workflow

import (
	"bytes"
	"fmt"

	"gopkg.in/yaml.v3"

	"github.com/tufantunc/RuntimePulse/internal/core"
)

type Selector struct {
	Type   string `yaml:"type"`
	Source string `yaml:"source"`
}

type Step struct {
	Label  string   `yaml:"label"`
	On     Selector `yaml:"on"`
	Prompt string   `yaml:"prompt"`
}

type Workflow struct {
	Session string `yaml:"session"`
	Agent   string `yaml:"agent"`
	// Repo is the session's repoPath; empty means "the directory apply
	// runs in" (resolved by the CLI, not here — this package is pure).
	Repo  string `yaml:"repo"`
	Steps []Step `yaml:"steps"`
}

// Parse decodes and validates a workflow document. Unknown YAML fields
// are errors (typo protection); missing labels default to step-N;
// prompt templates are syntax-checked (execution errors surface later
// as failed continuations, same contract as rule.add).
func Parse(data []byte) (Workflow, error) {
	var w Workflow
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&w); err != nil {
		return w, fmt.Errorf("workflow: bad field or syntax: %w", err)
	}
	if w.Session == "" {
		return w, fmt.Errorf("workflow: session is required")
	}
	if w.Agent == "" {
		return w, fmt.Errorf("workflow: agent is required")
	}
	if len(w.Steps) == 0 {
		return w, fmt.Errorf("workflow: at least one step is required")
	}
	seen := map[string]bool{}
	for i := range w.Steps {
		s := &w.Steps[i]
		if s.Label == "" {
			s.Label = fmt.Sprintf("step-%d", i+1)
		}
		if seen[s.Label] {
			return w, fmt.Errorf("workflow: duplicate label %q", s.Label)
		}
		seen[s.Label] = true
		if s.On.Type == "" {
			return w, fmt.Errorf("workflow: step %q: on.type is required", s.Label)
		}
		if s.Prompt == "" {
			return w, fmt.Errorf("workflow: step %q: prompt is required", s.Label)
		}
		if err := core.ValidatePromptTemplate(s.Prompt); err != nil {
			return w, fmt.Errorf("workflow: step %q: bad prompt template: %w", s.Label, err)
		}
	}
	return w, nil
}

// Rules compiles the workflow into oneShot rules.
func (w Workflow) Rules() []core.Rule {
	out := make([]core.Rule, 0, len(w.Steps))
	for _, s := range w.Steps {
		out = append(out, core.Rule{
			Selector:       core.EventSelector{Type: s.On.Type, Source: s.On.Source},
			ActionKind:     core.ActionContinueSession,
			SessionID:      w.Session,
			PromptTemplate: s.Prompt,
			Label:          s.Label,
			OneShot:        true,
		})
	}
	return out
}
