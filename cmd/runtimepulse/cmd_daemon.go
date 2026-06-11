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
	return &cobra.Command{
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
			fmt.Printf("runtimepulse daemon %s listening on %s\n", daemon.Version, daemon.SocketPath(dir))
			return d.Serve(ctx)
		},
	}
}
