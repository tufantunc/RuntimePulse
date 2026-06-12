// Package mcpserver exposes RuntimePulse to AI agents as an MCP server.
// Every tool is a thin client over the daemon's unix socket; the server
// itself holds no state.
package mcpserver

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/tufantunc/RuntimePulse/internal/client"
	"github.com/tufantunc/RuntimePulse/internal/version"
)

// instructions is the server-level usage contract injected into the
// host's context. It teaches the release-and-resume pattern that the
// per-tool descriptions cannot carry alone: arm a rule, end the turn,
// get resumed — never poll.
const instructions = `RuntimePulse resumes YOUR session when a runtime condition is met — you never poll.

Primary pattern (release-and-resume):
1. Call create_rule with YOUR OWN session id (pass agent + repoPath to register yourself in the same call; use oneShot:true unless you want to be woken on every match).
2. If the event needs a watcher, call create_watch AFTER create_rule — a new watch immediately emits the current state, so the rule must already exist to catch an "already ready" condition.
3. End your turn. When the event fires, RuntimePulse resumes your session with the rendered prompt as a new user turn. Do not wait, sleep, or poll for it.

Use wait_for_event ONLY for conditions expected within seconds — it blocks your current turn and sees only events published after the call. For anything longer or already-satisfied, use create_rule (+ create_watch) and end your turn.`

// New builds the MCP server. Tools are registered in registerTools
// (Tasks 2–4); the skeleton registers none yet.
func New(c *client.Client) *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{Name: "runtimepulse", Version: version.String()},
		&mcp.ServerOptions{Instructions: instructions})
	registerTools(srv, c)
	return srv
}

// registerTools wires the five tools; filled in over Tasks 2–4.
func registerTools(srv *mcp.Server, c *client.Client) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: "create_watch", Description: "Watch a runtime condition. Emits the current state immediately on creation, " +
			"then events on state transitions — so create the matching rule BEFORE the watch to catch an already-met condition.",
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
			"resume sessionId with the rendered prompt. Use YOUR OWN session id; pass agent+repoPath to register the session " +
			"in the same call. Prefer oneShot:true for wake-me-once. After creating the rule, END YOUR TURN — " +
			"you will be resumed with the prompt as a new turn; do not poll or wait.",
	}, createRuleHandler(c))
	mcp.AddTool(srv, &mcp.Tool{
		Name: "wait_for_event",
		Description: "Block your current turn until a matching event is published (live only; events before the call " +
			"are not seen), or until timeoutSeconds elapses. Use only for conditions expected within seconds. " +
			"For 'is it already ready?' or longer waits, prefer create_rule + create_watch and end your turn.",
	}, waitForEventHandler(c))
}
