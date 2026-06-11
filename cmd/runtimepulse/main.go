// runtimepulse — event-driven session continuation engine for AI coding agents.
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/tufantunc/RuntimePulse/internal/client"
	"github.com/tufantunc/RuntimePulse/internal/daemon"
)

func main() {
	root := &cobra.Command{
		Use:           "runtimepulse",
		Short:         "Event-driven session continuation engine for AI coding agents",
		Version:       daemon.Version,
		SilenceUsage:  true,
		SilenceErrors: false,
	}
	root.AddCommand(daemonCmd(), statusCmd(), eventsCmd(), ruleCmd(), sessionCmd(), injectCmd())
	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

// dial returns a client, auto-starting the daemon if needed.
func dial() (*client.Client, error) {
	dir, err := daemon.StateDir()
	if err != nil {
		return nil, err
	}
	return client.EnsureDaemon(dir, daemon.SocketPath(dir))
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
