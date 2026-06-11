package core

import (
	"strings"
	"text/template"
	"time"
)

// ActionContinueSession is the only action kind in the core plan;
// the field exists because the spec defines action.kind as extensible.
const ActionContinueSession = "continue_session"

// EventSelector matches events by type and (optionally) source.
type EventSelector struct {
	Type   string `json:"type"`
	Source string `json:"source,omitempty"` // empty matches any source
}

func (s EventSelector) Matches(ev Event) bool {
	if s.Type != ev.Type {
		return false
	}
	return s.Source == "" || s.Source == ev.Source
}

// Rule binds an event selector to a continuation action.
type Rule struct {
	ID             string        `json:"id"`
	Selector       EventSelector `json:"eventSelector"`
	ActionKind     string        `json:"actionKind"`
	SessionID      string        `json:"sessionId"`
	PromptTemplate string        `json:"promptTemplate"`
	Label          string        `json:"label,omitempty"`
	OneShot        bool          `json:"oneShot"`
	Consumed       bool          `json:"consumed"`
	ExpiresAt      *time.Time    `json:"expiresAt,omitempty"`
	CreatedAt      time.Time     `json:"createdAt"`
}

func (r Rule) Active(now time.Time) bool {
	if r.Consumed {
		return false
	}
	return r.ExpiresAt == nil || now.Before(*r.ExpiresAt)
}

// RenderPrompt renders the rule's prompt template with the event as
// {{.Event}}. Only machine-generated event fields enter the context.
func (r Rule) RenderPrompt(ev Event) (string, error) {
	t, err := template.New("prompt").Option("missingkey=error").Parse(r.PromptTemplate)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	if err := t.Execute(&b, struct{ Event Event }{ev}); err != nil {
		return "", err
	}
	return b.String(), nil
}

// ValidatePromptTemplate checks template syntax WITHOUT executing it.
// Execution-time errors (missing payload keys) are legitimate at
// validation time — they surface later as failed continuations.
func ValidatePromptTemplate(s string) error {
	_, err := template.New("prompt").Parse(s)
	return err
}
