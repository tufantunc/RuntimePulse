package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/tufantunc/RuntimePulse/internal/setup"
)

func setupCmd() *cobra.Command {
	var all, yes, project, dryRun bool
	var only string
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Detect installed agent CLIs and register RuntimePulse as their MCP server",
		RunE: func(cmd *cobra.Command, args []string) error {
			binPath, err := os.Executable()
			if err != nil {
				return err
			}
			home, err := os.UserHomeDir()
			if err != nil {
				return err
			}
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			scope := setup.ScopeUser
			if project {
				scope = setup.ScopeProject
			}

			agents := setup.Registry(home, cwd)
			if only != "" {
				known := false
				var names []string
				for _, a := range agents {
					names = append(names, a.Name())
					if a.Name() == only {
						known = true
					}
				}
				if !known {
					return fmt.Errorf("unknown agent %q (valid: %s)", only, strings.Join(names, "|"))
				}
			}
			statuses := setup.DetectAll(agents, scope)

			fmt.Println("Scanning for installed agent CLIs…")
			var installed []string
			for _, s := range statuses {
				mark, label := "✗", "not found"
				if s.Status.Installed {
					mark = "✓"
					label = "not registered"
					if s.Status.Registered {
						label = "already registered"
					}
					if only == "" || only == s.Agent.Name() {
						installed = append(installed, s.Agent.Name())
					}
				}
				fmt.Printf("  %s %-10s %s\n", mark, s.Agent.Name(), label)
			}
			if only != "" {
				installed = filter(installed, only)
			}
			if len(installed) == 0 {
				fmt.Println("\nNo target agent CLIs found.")
				return nil
			}

			if dryRun {
				fmt.Printf("\n[dry-run] would register RuntimePulse (%s scope) for: %s\n",
					scope.String(), strings.Join(installed, ", "))
				return nil
			}

			if !all && !yes && !confirm(installed) {
				fmt.Println("Aborted.")
				return nil
			}

			fmt.Println()
			failed := false
			for _, r := range setup.Run(agents, installed, binPath, scope) {
				switch {
				case r.Err != nil:
					failed = true
					fmt.Printf("  %-10s ✗ %v\n", r.Agent, r.Err)
				case r.Skipped:
					fmt.Printf("  %-10s • already registered\n", r.Agent)
				default:
					line := r.Agent + "  ✓ registered"
					if r.Note != "" {
						line += " (" + r.Note + ")"
					}
					fmt.Printf("  %s\n", line)
				}
			}
			fmt.Println("\nDone. Restart any running agent sessions to pick up the new MCP server.")
			if failed {
				return fmt.Errorf("one or more agents failed to register")
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "register every installed agent without prompting")
	cmd.Flags().BoolVar(&yes, "yes", false, "assume yes to the confirmation prompt")
	cmd.Flags().StringVar(&only, "agent", "", "target a single agent (claude|cursor|codex|opencode)")
	cmd.Flags().BoolVar(&project, "project", false, "register in the current project instead of user scope")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show what would happen, write nothing")
	return cmd
}

func filter(names []string, only string) []string {
	var out []string
	for _, n := range names {
		if n == only {
			out = append(out, n)
		}
	}
	return out
}

// confirm prompts on a TTY; with no TTY it returns false (use --all/--yes).
func confirm(names []string) bool {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Fprintln(os.Stderr, "not a TTY; pass --all or --yes to register non-interactively")
		return false
	}
	fmt.Printf("\nRegister RuntimePulse (MCP) for %s? [Y/n] ", strings.Join(names, ", "))
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	line = strings.TrimSpace(strings.ToLower(line))
	return line == "" || line == "y" || line == "yes"
}
