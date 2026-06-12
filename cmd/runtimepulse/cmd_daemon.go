package main

import (
	"context"
	"fmt"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/tufantunc/RuntimePulse/internal/daemon"
)

func daemonCmd() *cobra.Command {
	var wsPort int
	c := &cobra.Command{
		Use:   "daemon",
		Short: "Run the RuntimePulse daemon in the foreground",
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := daemon.StateDir()
			if err != nil {
				return err
			}
			d, err := daemon.New(dir)
			if err != nil {
				return fmt.Errorf("starting daemon: %w", err)
			}
			defer d.Close()
			ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()
			if wsPort > 0 {
				if _, err := d.ServeWS(ctx, wsPort); err != nil {
					return fmt.Errorf("starting ws server: %w", err)
				}
			}
			fmt.Printf("runtimepulse daemon %s listening on %s\n", daemon.Version, daemon.SocketPath(dir))
			return d.Serve(ctx)
		},
	}
	c.Flags().IntVar(&wsPort, "ws-port", 0, "opt-in WebSocket event stream port (0 = disabled)")
	return c
}
