package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

func ruleCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "rule", Short: "Manage rules"}
	cmd.AddCommand(ruleAddCmd(), ruleListCmd(), ruleRemoveCmd())
	return cmd
}

func ruleAddCmd() *cobra.Command {
	var on, session, agent, repo, prompt, label string
	var oneShot bool
	var expires time.Duration
	cmd := &cobra.Command{
		Use:   "add",
		Short: "Add a rule: when an event matches, continue a session",
		RunE: func(cmd *cobra.Command, args []string) error {
			evType, evSource, ok := strings.Cut(on, ":") // "docker.healthy:postgres" or "docker.healthy"
			if !ok {
				evType, evSource = on, ""
			}
			if evType == "" {
				return fmt.Errorf("--on is required (e.g. --on docker.healthy:postgres)")
			}
			params := map[string]any{
				"type": evType, "source": evSource, "sessionId": session,
				"agent": agent, "repoPath": repo, "prompt": prompt,
				"label": label, "oneShot": oneShot,
			}
			if expires > 0 {
				params["expiresAt"] = time.Now().UTC().Add(expires).Format(time.RFC3339)
			}
			c, err := dial()
			if err != nil {
				return err
			}
			var rule map[string]any
			if err := c.Call("rule.add", params, &rule); err != nil {
				return err
			}
			return printJSON(rule)
		},
	}
	cmd.Flags().StringVar(&on, "on", "", "event selector type[:source] (required)")
	cmd.Flags().StringVar(&session, "session", "", "target session id (required)")
	cmd.Flags().StringVar(&agent, "agent", "", "agent type; with --repo, registers the session")
	cmd.Flags().StringVar(&repo, "repo", "", "session repo path; with --agent, registers the session")
	cmd.Flags().StringVar(&prompt, "prompt", "", "prompt template (required)")
	cmd.Flags().StringVar(&label, "label", "", "rule label (becomes continuation event source)")
	cmd.Flags().BoolVar(&oneShot, "one-shot", false, "consume the rule after first match")
	cmd.Flags().DurationVar(&expires, "expires", 0, "rule TTL, e.g. 30m (0 = never)")
	cmd.MarkFlagRequired("on")
	cmd.MarkFlagRequired("session")
	cmd.MarkFlagRequired("prompt")
	return cmd
}

func ruleListCmd() *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List rules (JSON lines)",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := dial()
			if err != nil {
				return err
			}
			var rules []map[string]any
			if err := c.Call("rule.list", map[string]any{"all": all}, &rules); err != nil {
				return err
			}
			for _, r := range rules {
				if err := printJSON(r); err != nil {
					return err
				}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "include consumed and expired rules")
	return cmd
}

func ruleRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rm <rule-id>",
		Short: "Remove a rule",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := dial()
			if err != nil {
				return err
			}
			return c.Call("rule.remove", map[string]any{"id": args[0]}, nil)
		},
	}
}
