package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/tufantunc/RuntimePulse/internal/workflow"
)

// apply compiles a workflow file to rules (spec §7.3). The first
// rule.add self-registers the session (agent+repoPath). On a partial
// failure the already-created rules are removed best-effort, so a bad
// step never leaves half a workflow armed.
//
// Note: today the rollback path is unreachable with a Parse-valid
// workflow — the daemon re-validates the same things Parse checked.
// It is kept as defense for future server-side validations (e.g.
// agent-name checks); it has no integration test for that reason.
func applyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "apply <workflow.yaml>",
		Short: "Compile a workflow file into rules",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			data, err := os.ReadFile(args[0])
			if err != nil {
				return err
			}
			w, err := workflow.Parse(data)
			if err != nil {
				return err
			}
			repo := w.Repo
			if repo == "" {
				if repo, err = os.Getwd(); err != nil {
					return err
				}
			}
			abs, err := filepath.Abs(repo)
			if err != nil {
				return err
			}
			c, err := dial()
			if err != nil {
				return err
			}
			var created []string
			for _, r := range w.Rules() {
				var out map[string]any
				if err := c.Call("rule.add", map[string]any{
					"type": r.Selector.Type, "source": r.Selector.Source,
					"sessionId": r.SessionID, "agent": w.Agent, "repoPath": abs,
					"prompt": r.PromptTemplate, "label": r.Label, "oneShot": true,
				}, &out); err != nil {
					for _, id := range created {
						c.Call("rule.remove", map[string]any{"id": id}, nil) //nolint:errcheck // best-effort rollback
					}
					return fmt.Errorf("apply: step %q failed: %w (rolled back %d created rules)",
						r.Label, err, len(created))
				}
				if id, ok := out["id"].(string); ok {
					created = append(created, id)
				}
				printJSON(out)
			}
			return nil
		},
	}
}
