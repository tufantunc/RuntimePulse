package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
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
			var names []string
			for _, a := range agents {
				names = append(names, a.Name())
			}
			var targets []string // nil means "no --agent filter"
			if only != "" {
				targets, err = parseAgentFlag(only, names)
				if err != nil {
					return err
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
					if targets == nil || contains(targets, s.Agent.Name()) {
						installed = append(installed, s.Agent.Name())
					}
				}
				fmt.Printf("  %s %-10s %s\n", mark, s.Agent.Name(), label)
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

			if !all && !yes && targets == nil {
				chosen, err := selectAgents(installed)
				if err != nil {
					return err
				}
				if chosen == nil {
					fmt.Println("Aborted.")
					return nil
				}
				installed = chosen
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
					line := fmt.Sprintf("%-10s ✓ registered (%s scope)", r.Agent, scope.String())
					if r.Note != "" {
						line = fmt.Sprintf("%-10s ✓ registered (%s)", r.Agent, r.Note)
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
	cmd.Flags().StringVar(&only, "agent", "", "target specific agents, comma-separated (claude|cursor|codex|opencode)")
	cmd.Flags().BoolVar(&project, "project", false, "register in the current project instead of user scope")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show what would happen, write nothing")
	return cmd
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// parseAgentFlag splits a comma-separated --agent value, trims whitespace,
// validates each name against valid, and dedupes while preserving order.
func parseAgentFlag(value string, valid []string) ([]string, error) {
	var out []string
	for _, tok := range strings.Split(value, ",") {
		tok = strings.TrimSpace(tok)
		if tok == "" || !contains(valid, tok) {
			return nil, fmt.Errorf("unknown agent %q (valid: %s)", tok, strings.Join(valid, "|"))
		}
		if !contains(out, tok) {
			out = append(out, tok)
		}
	}
	return out, nil
}

// parseSelection interprets one line of interactive input against candidates.
// Contract: empty/whitespace input returns all candidates; "q" or "n" returns
// (nil, nil) signaling quit; otherwise a comma-separated, whitespace-tolerant
// mix of 1-based numbers and candidate names, deduped in input order.
func parseSelection(input string, candidates []string) ([]string, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return append([]string(nil), candidates...), nil
	}
	low := strings.ToLower(input)
	if low == "q" || low == "n" {
		return nil, nil
	}
	var out []string
	for _, tok := range strings.Split(input, ",") {
		tok = strings.TrimSpace(tok)
		name := ""
		if n, err := strconv.Atoi(tok); err == nil {
			if n < 1 || n > len(candidates) {
				return nil, fmt.Errorf("number %d out of range (1-%d)", n, len(candidates))
			}
			name = candidates[n-1]
		} else if contains(candidates, tok) {
			name = tok
		} else {
			return nil, fmt.Errorf("unknown selection %q", tok)
		}
		if !contains(out, name) {
			out = append(out, name)
		}
	}
	return out, nil
}

// selectAgents shows a numbered picker on a TTY; with no TTY it returns
// (nil, nil) — same as a user quit — after hinting at --all/--yes.
// One invalid input re-prompts; a second invalid input returns an error.
func selectAgents(candidates []string) ([]string, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Fprintln(os.Stderr, "not a TTY; pass --all or --yes to register non-interactively")
		return nil, nil
	}
	fmt.Println("\nRegister RuntimePulse (MCP) for which agents?")
	for i, name := range candidates {
		fmt.Printf("  [%d] %s\n", i+1, name)
	}
	reader := bufio.NewReader(os.Stdin)
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		fmt.Print("Selection (e.g. 1,3 — empty = all, q = quit): ")
		line, _ := reader.ReadString('\n')
		chosen, err := parseSelection(line, candidates)
		if err != nil {
			lastErr = err
			fmt.Fprintf(os.Stderr, "invalid selection: %v\n", err)
			continue
		}
		return chosen, nil
	}
	return nil, fmt.Errorf("aborting after repeated invalid selection: %w", lastErr)
}
