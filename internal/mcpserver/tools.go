package mcpserver

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/tufantunc/RuntimePulse/internal/client"
	"github.com/tufantunc/RuntimePulse/internal/core"
)

// --- create_watch -----------------------------------------------------

type CreateWatchInput struct {
	Type      string `json:"type" jsonschema:"watch type: http, tcp, file, process, docker or git"`
	Target    string `json:"target" jsonschema:"what to watch (url, host:port, path, pgrep pattern, container, or repo)"`
	Interval  string `json:"interval,omitempty" jsonschema:"poll interval as a Go duration, e.g. 2s"`
	Stability string `json:"stability,omitempty" jsonschema:"flap-suppression threshold as a Go duration, e.g. 5s"`
}

type WatchOutput struct {
	ID     string `json:"id"`
	Type   string `json:"type"`
	Target string `json:"target"`
}

func createWatchHandler(c *client.Client) mcp.ToolHandlerFor[CreateWatchInput, WatchOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in CreateWatchInput) (*mcp.CallToolResult, WatchOutput, error) {
		var out WatchOutput
		err := c.Call("watch.add", map[string]any{
			"type": in.Type, "target": in.Target,
			"interval": in.Interval, "stability": in.Stability,
		}, &out)
		return nil, out, err
	}
}

// --- get_events -------------------------------------------------------

type GetEventsInput struct {
	Type  string `json:"type,omitempty" jsonschema:"filter by event type, e.g. docker.healthy"`
	Limit int    `json:"limit,omitempty" jsonschema:"max events to return (default 100)"`
}

type GetEventsOutput struct {
	Events []core.Event `json:"events"`
}

func getEventsHandler(c *client.Client) mcp.ToolHandlerFor[GetEventsInput, GetEventsOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in GetEventsInput) (*mcp.CallToolResult, GetEventsOutput, error) {
		var out GetEventsOutput
		err := c.Call("events.list", map[string]any{"type": in.Type, "limit": in.Limit}, &out.Events)
		return nil, out, err
	}
}

// --- cancel_rule ------------------------------------------------------

type CancelRuleInput struct {
	RuleID string `json:"ruleId" jsonschema:"id of the rule to cancel"`
}

type OKOutput struct {
	OK bool `json:"ok"`
}

func cancelRuleHandler(c *client.Client) mcp.ToolHandlerFor[CancelRuleInput, OKOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in CancelRuleInput) (*mcp.CallToolResult, OKOutput, error) {
		if err := c.Call("rule.remove", map[string]any{"id": in.RuleID}, nil); err != nil {
			return nil, OKOutput{}, err
		}
		return nil, OKOutput{OK: true}, nil
	}
}

// --- create_rule ------------------------------------------------------

type CreateRuleInput struct {
	EventType string `json:"eventType" jsonschema:"event type to match, e.g. docker.healthy or continuation.completed"`
	Source    string `json:"source,omitempty" jsonschema:"optional event source filter, e.g. a container name"`
	SessionID string `json:"sessionId" jsonschema:"the agent session to resume when the event matches"`
	Agent     string `json:"agent,omitempty" jsonschema:"agent type (claude|cursor|codex|opencode); with repoPath, self-registers the session"`
	RepoPath  string `json:"repoPath,omitempty" jsonschema:"session repo path; with agent, self-registers the session"`
	Prompt    string `json:"prompt" jsonschema:"Go text/template prompt; only event fields are available, e.g. {{.Event.Source}}"`
	Label     string `json:"label,omitempty" jsonschema:"rule label; becomes the source of the continuation.* result event"`
	OneShot   bool   `json:"oneShot,omitempty" jsonschema:"consume the rule after its first match"`
	ExpiresAt string `json:"expiresAt,omitempty" jsonschema:"optional RFC3339 expiry"`
}

type RuleOutput struct {
	ID        string `json:"id"`
	SessionID string `json:"sessionId"`
}

func createRuleHandler(c *client.Client) mcp.ToolHandlerFor[CreateRuleInput, RuleOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in CreateRuleInput) (*mcp.CallToolResult, RuleOutput, error) {
		var out RuleOutput
		err := c.Call("rule.add", map[string]any{
			"type": in.EventType, "source": in.Source,
			"sessionId": in.SessionID, "agent": in.Agent, "repoPath": in.RepoPath,
			"prompt": in.Prompt, "label": in.Label, "oneShot": in.OneShot, "expiresAt": in.ExpiresAt,
		}, &out)
		return nil, out, err
	}
}
