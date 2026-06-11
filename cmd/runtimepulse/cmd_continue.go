package main

import "github.com/spf13/cobra"

// continue: spec §7.1's manual trigger — enqueue a continuation for a
// registered session without waiting for an event.
func continueCmd() *cobra.Command {
	var session, prompt string
	cmd := &cobra.Command{
		Use:   "continue",
		Short: "Manually resume a registered session with a prompt",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := dial()
			if err != nil {
				return err
			}
			var cont map[string]any
			if err := c.Call("continuation.run", map[string]any{
				"sessionId": session, "prompt": prompt,
			}, &cont); err != nil {
				return err
			}
			return printJSON(cont)
		},
	}
	cmd.Flags().StringVar(&session, "session", "", "registered session id (required)")
	cmd.Flags().StringVar(&prompt, "prompt", "", "prompt to inject (required)")
	cmd.MarkFlagRequired("session")
	cmd.MarkFlagRequired("prompt")
	return cmd
}

func continuationsCmd() *cobra.Command {
	var state string
	cmd := &cobra.Command{
		Use:   "continuations",
		Short: "List continuations (JSON lines)",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := dial()
			if err != nil {
				return err
			}
			var list []map[string]any
			if err := c.Call("continuation.list", map[string]any{"state": state}, &list); err != nil {
				return err
			}
			for _, item := range list {
				if err := printJSON(item); err != nil {
					return err
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&state, "state", "", "filter: pending|running|completed|failed")
	return cmd
}
