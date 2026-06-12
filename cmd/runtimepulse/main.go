// runtimepulse — event-driven session continuation engine for AI coding agents.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tufantunc/RuntimePulse/internal/client"
	"github.com/tufantunc/RuntimePulse/internal/daemon"
	"github.com/tufantunc/RuntimePulse/internal/version"
)

func main() {
	root := &cobra.Command{
		Use:           "runtimepulse",
		Short:         "Event-driven session continuation engine for AI coding agents",
		Version:       version.String(),
		SilenceUsage:  true,
		SilenceErrors: false,
	}
	root.AddCommand(daemonCmd(), statusCmd(), eventsCmd(), ruleCmd(), sessionCmd(), injectCmd(), watchCmd(), execCmd(), continueCmd(), continuationsCmd(), mcpCmd(), applyCmd(), setupCmd())
	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

// dial returns a client, auto-starting the daemon if needed, and warns
// once on a daemon/client version mismatch.
func dial() (*client.Client, error) {
	dir, err := daemon.StateDir()
	if err != nil {
		return nil, err
	}
	c, err := client.EnsureDaemon(dir, daemon.SocketPath(dir))
	if err != nil {
		return nil, err
	}
	warnVersionMismatch(c)
	return c, nil
}

// warnVersionMismatch never fails the command and stays silent for dev
// builds on either side — only released-vs-released mismatches matter.
func warnVersionMismatch(c *client.Client) {
	local := version.String()
	if strings.Contains(local, "dev") {
		return
	}
	var st struct {
		Version string `json:"version"`
	}
	if err := c.Call("status", nil, &st); err != nil {
		return
	}
	if st.Version == "" || strings.Contains(st.Version, "dev") || st.Version == local {
		return
	}
	fmt.Fprintf(os.Stderr,
		"warning: daemon is %s but this client is %s — stop the daemon (it auto-restarts on next use) to update\n",
		st.Version, local)
}

// printJSON writes v as one JSON line to stdout (spec §7.1 output format).
func printJSON(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	fmt.Println(string(b))
	return nil
}
