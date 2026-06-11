package main

import "github.com/spf13/cobra"

func statusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show daemon status",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := dial()
			if err != nil {
				return err
			}
			var st map[string]any
			if err := c.Call("status", nil, &st); err != nil {
				return err
			}
			return printJSON(st)
		},
	}
}
