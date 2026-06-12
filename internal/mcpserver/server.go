// Package mcpserver exposes RuntimePulse to AI agents as an MCP server.
// Every tool is a thin client over the daemon's unix socket; the server
// itself holds no state.
package mcpserver

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/tufantunc/RuntimePulse/internal/client"
	"github.com/tufantunc/RuntimePulse/internal/version"
)

// New builds the MCP server. Tools are registered in registerTools
// (Tasks 2–4); the skeleton registers none yet.
func New(c *client.Client) *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{Name: "runtimepulse", Version: version.String()}, nil)
	registerTools(srv, c)
	return srv
}

// registerTools wires the five tools; filled in over Tasks 2–4.
func registerTools(srv *mcp.Server, c *client.Client) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: "create_watch", Description: "Watch a runtime condition; emits events on state transitions.",
	}, createWatchHandler(c))
	mcp.AddTool(srv, &mcp.Tool{
		Name: "get_events", Description: "Query recent events, newest first, optionally filtered by type.",
	}, getEventsHandler(c))
	mcp.AddTool(srv, &mcp.Tool{
		Name: "cancel_rule", Description: "Cancel a pending rule by id.",
	}, cancelRuleHandler(c))
	mcp.AddTool(srv, &mcp.Tool{
		Name: "create_rule",
		Description: "Bind an event to a session resume: when eventType (optionally from source) fires, " +
			"resume sessionId with the rendered prompt. Pass agent+repoPath to register the session in the same call.",
	}, createRuleHandler(c))
	mcp.AddTool(srv, &mcp.Tool{
		Name: "wait_for_event",
		Description: "Block until a matching event is published (live only; events before the call are not seen), " +
			"or until timeoutSeconds elapses. For 'is it already ready?', prefer create_watch + create_rule.",
	}, waitForEventHandler(c))
}
