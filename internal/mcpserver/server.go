// Package mcpserver exposes RuntimePulse to AI agents as an MCP server.
// Every tool is a thin client over the daemon's unix socket; the server
// itself holds no state.
package mcpserver

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/tufantunc/RuntimePulse/internal/client"
)

// Version is reported to MCP clients during initialize.
const Version = "0.1.0-dev"

// New builds the MCP server. Tools are registered in registerTools
// (Tasks 2–4); the skeleton registers none yet.
func New(c *client.Client) *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{Name: "runtimepulse", Version: Version}, nil)
	registerTools(srv, c)
	return srv
}

// registerTools wires the five tools; filled in over Tasks 2–4.
func registerTools(srv *mcp.Server, c *client.Client) {}
