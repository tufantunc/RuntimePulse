package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/tufantunc/RuntimePulse/internal/core"
	"github.com/tufantunc/RuntimePulse/internal/version"
	"github.com/tufantunc/RuntimePulse/internal/watch"
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
	case "continuation.run":
		p, err := unmarshalParams[struct {
			SessionID string `json:"sessionId"`
			Prompt    string `json:"prompt"`
		}](params)
		if err != nil {
			return nil, err
		}
		if p.SessionID == "" || p.Prompt == "" {
			return nil, errors.New("continuation.run: sessionId and prompt are required")
		}
		if _, ok, err := d.Engine.Store.GetSession(p.SessionID); err != nil {
			return nil, err
		} else if !ok {
			return nil, fmt.Errorf("continuation.run: unknown session %q", p.SessionID)
		}
		// A manual continuation is rule-less: synthetic unique ids keep
		// the (rule_id, event_id) idempotency constraint satisfied.
		c, err := d.Engine.Store.InsertManualContinuation(p.SessionID, p.Prompt)
		if err != nil {
			return nil, err
		}
		d.Dispatch.Wake()
		return c, nil
	case "watch.add":
		p, err := unmarshalParams[struct {
			Type      string `json:"type"`
			Target    string `json:"target"`
			Interval  string `json:"interval"`  // Go duration, optional
			Stability string `json:"stability"` // Go duration, optional
		}](params)
		if err != nil {
			return nil, err
		}
		if err := watch.ValidateType(p.Type); err != nil {
			return nil, err
		}
		if p.Target == "" {
			return nil, errors.New("watch.add: target is required")
		}
		if p.Type == "docker" {
			if err := watch.DockerAvailable(); err != nil {
				return nil, fmt.Errorf("watch.add: docker CLI not found: %w", err)
			}
		}
		if p.Type == "git" {
			if fi, statErr := os.Stat(filepath.Join(p.Target, ".git")); statErr != nil || !fi.IsDir() {
				return nil, fmt.Errorf("watch.add: %s is not a git repository", p.Target)
			}
		}
		var cfg watch.Config
		if p.Interval != "" {
			dur, err := time.ParseDuration(p.Interval)
			if err != nil {
				return nil, fmt.Errorf("watch.add: bad interval: %w", err)
			}
			cfg.Interval = dur
		}
		if p.Stability != "" {
			dur, err := time.ParseDuration(p.Stability)
			if err != nil {
				return nil, fmt.Errorf("watch.add: bad stability: %w", err)
			}
			cfg.StabilityThreshold = dur
		}
		w, err := d.Engine.Store.AddWatch(watch.Watch{Type: p.Type, Target: p.Target, Config: cfg})
		if err != nil {
			return nil, err
		}
		if err := d.Watches.Start(w); err != nil {
			return nil, err
		}
		return w, nil
	case "watch.list":
		return d.Engine.Store.ListWatches()
	case "watch.remove":
		p, err := unmarshalParams[struct {
			ID string `json:"id"`
		}](params)
		if err != nil {
			return nil, err
		}
		d.Watches.Stop(p.ID)
		return "ok", d.Engine.Store.RemoveWatch(p.ID)
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
	running, err := d.Engine.Store.ListContinuations("running")
	if err != nil {
		return nil, err
	}
	watches, err := d.Engine.Store.ListWatches()
	if err != nil {
		return nil, err
	}
	lastEvent := ""
	if len(events) > 0 {
		lastEvent = events[0].Type + " @ " + events[0].Timestamp.Format(time.RFC3339)
	}
	return map[string]any{
		"version":              version.String(),
		"rules":                len(rules),
		"sessions":             len(sessions),
		"pendingContinuations": len(pending),
		"runningContinuations": len(running),
		"watches":              len(watches),
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

	// Syntax-check the template before any side effects so a malformed
	// rule fails at creation without registering the session.
	if err := core.ValidatePromptTemplate(p.Prompt); err != nil {
		return nil, fmt.Errorf("rule.add: bad prompt template: %w", err)
	}

	// Parse expiresAt before any side effects too.
	var expiresAt *time.Time
	if p.ExpiresAt != "" {
		t, err := time.Parse(time.RFC3339, p.ExpiresAt)
		if err != nil {
			return nil, fmt.Errorf("rule.add: bad expiresAt: %w", err)
		}
		expiresAt = &t
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
		ExpiresAt:      expiresAt,
	}
	return d.Engine.Store.AddRule(r)
}

type notification struct {
	Method string     `json:"method"`
	Params core.Event `json:"params"`
}

// follow streams bus events as notifications until the client
// disconnects. Bus drops to slow consumers (no gap signal) — clients
// that need completeness reconcile via events.list.
func (d *Daemon) follow(conn net.Conn, enc *json.Encoder, req request) {
	ch, cancel := d.bus.Subscribe(64)
	defer cancel()
	if err := enc.Encode(response{ID: req.ID, Result: "ok"}); err != nil {
		return
	}
	// Detect client disconnect: the client never sends more data on a
	// follow connection, so a read returning is a hang-up.
	done := make(chan struct{})
	go func() {
		buf := make([]byte, 1)
		for {
			if _, err := conn.Read(buf); err != nil {
				close(done)
				return
			}
		}
	}()
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				return
			}
			if err := enc.Encode(notification{Method: "event", Params: ev}); err != nil {
				return
			}
		case <-done:
			return
		}
	}
}
