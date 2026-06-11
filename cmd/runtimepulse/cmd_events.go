package main

import (
	"context"

	"github.com/spf13/cobra"
	"github.com/tufantunc/RuntimePulse/internal/core"
)

func eventsCmd() *cobra.Command {
	var follow bool
	var evType string
	var limit int
	cmd := &cobra.Command{
		Use:   "events",
		Short: "List stored events (JSON lines), or stream live with --follow",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := dial()
			if err != nil {
				return err
			}
			if follow {
				return c.Follow(context.Background(), func(ev core.Event) {
					printJSON(ev)
				})
			}
			var evs []core.Event
			if err := c.Call("events.list",
				map[string]any{"type": evType, "limit": limit}, &evs); err != nil {
				return err
			}
			for _, ev := range evs {
				if err := printJSON(ev); err != nil {
					return err
				}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&follow, "follow", false, "stream events live")
	cmd.Flags().StringVar(&evType, "type", "", "filter by event type")
	cmd.Flags().IntVar(&limit, "limit", 100, "max events to list")
	return cmd
}
