package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"

	"github.com/tufantunc/RuntimePulse/internal/mcpserver"
)

// mcpCmd runs the MCP server over stdio. It is launched by an agent CLI,
// e.g.  claude mcp add --transport stdio runtimepulse -- runtimepulse mcp
// stdout carries the MCP protocol stream; all logging goes to stderr.
func mcpCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "Run the RuntimePulse MCP server over stdio",
		RunE: func(cmd *cobra.Command, args []string) error {
			log.SetOutput(os.Stderr) // never write to stdout: it is the protocol stream
			c, err := dial()         // EnsureDaemon: auto-starts the shared daemon
			if err != nil {
				return err
			}
			ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()
			return mcpserver.New(c).Run(ctx, &mcp.StdioTransport{})
		},
	}
}
