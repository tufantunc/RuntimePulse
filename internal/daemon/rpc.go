package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/tufantunc/RuntimePulse/internal/core"
)

type request struct {
	ID     int64           `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

type response struct {
	ID     int64  `json:"id"`
	Result any    `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}

func (d *Daemon) handleConn(conn net.Conn) {
	defer conn.Close()
	dec := json.NewDecoder(conn)
	enc := json.NewEncoder(conn)
	for {
		var req request
		if err := dec.Decode(&req); err != nil {
			return // client closed or sent garbage; drop the connection
		}
		if req.Method == "events.follow" {
			d.follow(conn, enc, req)
			return
		}
		result, err := d.dispatch(req.Method, req.Params)
		resp := response{ID: req.ID, Result: result}
		if err != nil {
			resp = response{ID: req.ID, Error: err.Error()}
		}
		if err := enc.Encode(resp); err != nil {
			return
		}
	}
}

func unmarshalParams[T any](raw json.RawMessage) (T, error) {
	var p T
	if len(raw) == 0 {
		return p, nil
	}
	err := json.Unmarshal(raw, &p)
	return p, err
}

func (d *Daemon) dispatch(method string, params json.RawMessage) (any, error) {
	switch method {
	case "status":
		return d.status()
	case "event.inject":
		p, err := unmarshalParams[struct {
			Type    string            `json:"type"`
			Source  string            `json:"source"`
			Payload map[string]string `json:"payload"`
		}](params)
		if err != nil {
			return nil, err
		}
		if p.Type == "" {
			return nil, errors.New("event.inject: type is required")
		}
		return d.Engine.Ingest(core.Event{Type: p.Type, Source: p.Source, Payload: p.Payload})
	case "events.list":
		p, err := unmarshalParams[struct {
			Type  string `json:"type"`
			Limit int    `json:"limit"`
		}](params)
		if err != nil {
			return nil, err
		}
		return d.Engine.Store.ListEvents(p.Type, p.Limit)
	case "rule.add":
		return d.ruleAdd(params)
	case "rule.list":
		p, err := unmarshalParams[struct {
			All bool `json:"all"`
		}](params)
		if err != nil {
			return nil, err
		}
		return d.Engine.Store.ListRules(p.All)
	case "rule.remove":
		p, err := unmarshalParams[struct {
			ID string `json:"id"`
		}](params)
		if err != nil {
			return nil, err
		}
		return "ok", d.Engine.Store.RemoveRule(p.ID)
	case "session.register":
		p, err := unmarshalParams[core.Session](params)
		if err != nil {
			return nil, err
		}
		if p.SessionID == "" || p.Agent == "" {
			return nil, errors.New("session.register: sessionId and agent are required")
		}
		return d.Engine.Store.RegisterSession(p)
	case "session.list":
		return d.Engine.Store.ListSessions()
	case "continuation.list":
		p, err := unmarshalParams[struct {
			State string `json:"state"`
		}](params)
		if err != nil {
			return nil, err
		}
		return d.Engine.Store.ListContinuations(p.State)
	default:
		return nil, fmt.Errorf("unknown method %q", method)
	}
}

func (d *Daemon) status() (any, error) {
	events, err := d.Engine.Store.ListEvents("", 1)
	if err != nil {
		return nil, err
	}
	rules, err := d.Engine.Store.ListRules(false)
	if err != nil {
		return nil, err
	}
	sessions, err := d.Engine.Store.ListSessions()
	if err != nil {
		return nil, err
	}
	pending, err := d.Engine.Store.ListContinuations("pending")
	if err != nil {
		return nil, err
	}
	lastEvent := ""
	if len(events) > 0 {
		lastEvent = events[0].Type + " @ " + events[0].Timestamp.Format(time.RFC3339)
	}
	return map[string]any{
		"version":              Version,
		"rules":                len(rules),
		"sessions":             len(sessions),
		"pendingContinuations": len(pending),
		"lastEvent":            lastEvent,
	}, nil
}

func (d *Daemon) ruleAdd(params json.RawMessage) (any, error) {
	p, err := unmarshalParams[struct {
		Type      string `json:"type"`
		Source    string `json:"source"`
		SessionID string `json:"sessionId"`
		Agent     string `json:"agent"`
		RepoPath  string `json:"repoPath"`
		Prompt    string `json:"prompt"`
		Label     string `json:"label"`
		OneShot   bool   `json:"oneShot"`
		ExpiresAt string `json:"expiresAt"` // RFC3339, optional
	}](params)
	if err != nil {
		return nil, err
	}
	if p.Type == "" || p.SessionID == "" || p.Prompt == "" {
		return nil, errors.New("rule.add: type, sessionId and prompt are required")
	}

	// Self-registration path (spec §4.4): agent+repoPath registers the
	// session in the same call. Otherwise the session must already exist.
	if p.Agent != "" && p.RepoPath != "" {
		if _, err := d.Engine.Store.RegisterSession(core.Session{
			SessionID: p.SessionID, Agent: p.Agent, RepoPath: p.RepoPath,
		}); err != nil {
			return nil, err
		}
	} else if _, ok, err := d.Engine.Store.GetSession(p.SessionID); err != nil {
		return nil, err
	} else if !ok {
		return nil, fmt.Errorf("rule.add: unknown session %q (register it or pass agent+repoPath)", p.SessionID)
	}

	r := core.Rule{
		Selector:       core.EventSelector{Type: p.Type, Source: p.Source},
		ActionKind:     core.ActionContinueSession,
		SessionID:      p.SessionID,
		PromptTemplate: p.Prompt,
		Label:          p.Label,
		OneShot:        p.OneShot,
	}
	if p.ExpiresAt != "" {
		t, err := time.Parse(time.RFC3339, p.ExpiresAt)
		if err != nil {
			return nil, fmt.Errorf("rule.add: bad expiresAt: %w", err)
		}
		r.ExpiresAt = &t
	}
	// Syntax-check the template now so a malformed rule fails at
	// creation; execution errors surface later as failed continuations.
	if err := core.ValidatePromptTemplate(p.Prompt); err != nil {
		return nil, fmt.Errorf("rule.add: bad prompt template: %w", err)
	}
	return d.Engine.Store.AddRule(r)
}

func (d *Daemon) follow(conn net.Conn, enc *json.Encoder, req request) {
	enc.Encode(response{ID: req.ID, Error: "events.follow: not implemented yet"})
}
