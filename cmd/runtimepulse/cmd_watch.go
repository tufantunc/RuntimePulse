package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func watchCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "watch", Short: "Manage runtime condition watchers"}
	cmd.AddCommand(watchAddCmd(), watchListCmd(), watchRemoveCmd())
	return cmd
}

// watchAddCmd builds `watch add <type>` subcommands; each type has its
// own natural flag name for the target (spec §7.1).
func watchAddCmd() *cobra.Command {
	add := &cobra.Command{Use: "add", Short: "Add a watch"}
	for _, spec := range []struct{ typ, flag, usage string }{
		{"http", "url", "URL to probe (2xx/3xx = available)"},
		{"tcp", "addr", "host:port to probe"},
		{"file", "path", "file path to observe"},
		{"process", "pattern", "pgrep -f pattern"},
		{"docker", "container", "container name"},
		{"git", "repo", "repository path"},
	} {
		typ, flag := spec.typ, spec.flag
		var target, interval, stability string
		c := &cobra.Command{
			Use:   typ,
			Short: fmt.Sprintf("Watch a %s target", typ),
			RunE: func(cmd *cobra.Command, args []string) error {
				cl, err := dial()
				if err != nil {
					return err
				}
				var w map[string]any
				if err := cl.Call("watch.add", map[string]any{
					"type": typ, "target": target,
					"interval": interval, "stability": stability,
				}, &w); err != nil {
					return err
				}
				return printJSON(w)
			},
		}
		c.Flags().StringVar(&target, flag, "", spec.usage+" (required)")
		c.Flags().StringVar(&interval, "interval", "", "poll interval, e.g. 2s")
		c.Flags().StringVar(&stability, "stability", "", "flap suppression threshold, e.g. 5s")
		c.MarkFlagRequired(flag)
		add.AddCommand(c)
	}
	return add
}

func watchListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List watches (JSON lines)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, err := dial()
			if err != nil {
				return err
			}
			var ws []map[string]any
			if err := cl.Call("watch.list", nil, &ws); err != nil {
				return err
			}
			for _, w := range ws {
				if err := printJSON(w); err != nil {
					return err
				}
			}
			return nil
		},
	}
}

func watchRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rm <watch-id>",
		Short: "Remove a watch",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, err := dial()
			if err != nil {
				return err
			}
			return cl.Call("watch.remove", map[string]any{"id": args[0]}, nil)
		},
	}
}
