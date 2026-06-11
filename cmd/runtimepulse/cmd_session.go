package main

import "github.com/spf13/cobra"

func sessionCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "session", Short: "Manage the session registry"}
	cmd.AddCommand(sessionRegisterCmd(), sessionListCmd())
	return cmd
}

func sessionRegisterCmd() *cobra.Command {
	var agent, session, repo string
	cmd := &cobra.Command{
		Use:   "register",
		Short: "Register an agent session",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := dial()
			if err != nil {
				return err
			}
			var out map[string]any
			if err := c.Call("session.register", map[string]any{
				"sessionId": session, "agent": agent, "repoPath": repo,
			}, &out); err != nil {
				return err
			}
			return printJSON(out)
		},
	}
	cmd.Flags().StringVar(&agent, "agent", "", "agent type: claude|cursor|codex|opencode (required)")
	cmd.Flags().StringVar(&session, "session", "", "session id (required)")
	cmd.Flags().StringVar(&repo, "repo", "", "repository path (required)")
	cmd.MarkFlagRequired("agent")
	cmd.MarkFlagRequired("session")
	cmd.MarkFlagRequired("repo")
	return cmd
}

func sessionListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List registered sessions (JSON lines)",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := dial()
			if err != nil {
				return err
			}
			var sessions []map[string]any
			if err := c.Call("session.list", nil, &sessions); err != nil {
				return err
			}
			for _, s := range sessions {
				if err := printJSON(s); err != nil {
					return err
				}
			}
			return nil
		},
	}
}
