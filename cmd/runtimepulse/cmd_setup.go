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
	var all, yes, project, dryRun, rules, noRules bool
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
				if rules {
					fmt.Print("\n" + formatRuleResults(setup.WriteRules(agents, installed, home, true)))
				}
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
			interactive := term.IsTerminal(int(os.Stdin.Fd()))
			if shouldWriteRules(rules, noRules, interactive, func() bool {
				return promptYesNo("\nAdd the release-and-resume guidance to your agents' global instructions? [y/N]: ")
			}) {
				fmt.Println()
				out := formatRuleResults(setup.WriteRules(agents, installed, home, false))
				fmt.Print(out)
				if strings.Contains(out, "✗ rules:") {
					failed = true
				}
			}
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
	cmd.Flags().BoolVar(&rules, "rules", false, "also install the release-and-resume guidance into agents' global instructions")
	cmd.Flags().BoolVar(&noRules, "no-rules", false, "skip the global-instruction guidance step without prompting")
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
		if tok == "" {
			return nil, fmt.Errorf("empty agent name in %q", value)
		}
		if !contains(valid, tok) {
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

// shouldWriteRules decides whether the rules step runs. --no-rules always
// wins; then --rules; otherwise, interactively prompt (default N), and
// non-interactively default to skip.
func shouldWriteRules(rules, noRules, interactive bool, prompt func() bool) bool {
	switch {
	case noRules:
		return false
	case rules:
		return true
	case interactive:
		return prompt()
	default:
		return false
	}
}

// formatRuleResults renders the per-agent rule lines plus, if any agent is
// manual (cursor), the paste-into-Settings block.
func formatRuleResults(results []setup.RuleResult) string {
	var b strings.Builder
	var manual *setup.RuleResult
	for i := range results {
		r := results[i]
		switch {
		case r.Err != nil:
			fmt.Fprintf(&b, "  %-10s ✗ rules: %v\n", r.Agent, r.Err)
		case r.Manual:
			fmt.Fprintf(&b, "  %-10s • rules: paste manually (see below)\n", r.Agent)
			manual = &results[i]
		case r.Action == "unchanged":
			fmt.Fprintf(&b, "  %-10s • rules unchanged (%s)\n", r.Agent, r.Path)
		case r.Action == "would-write":
			fmt.Fprintf(&b, "  %-10s ✓ rules: would write (%s)\n", r.Agent, r.Path)
		default: // written | updated | appended
			fmt.Fprintf(&b, "  %-10s ✓ rules %s (%s)\n", r.Agent, r.Action, r.Path)
		}
	}
	if manual != nil {
		b.WriteString("\ncursor — paste this into Settings → Rules → User Rules:\n\n")
		for _, ln := range strings.Split(manual.Text, "\n") {
			fmt.Fprintf(&b, "    %s\n", ln)
		}
	}
	return b.String()
}

// promptYesNo reads a single y/N answer from stdin (default N).
func promptYesNo(prompt string) bool {
	fmt.Print(prompt)
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	line = strings.ToLower(strings.TrimSpace(line))
	return line == "y" || line == "yes"
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
		line, readErr := reader.ReadString('\n')
		if readErr != nil && strings.TrimSpace(line) == "" {
			// Ctrl-D / closed stdin is an abort, never "select all".
			return nil, nil
		}
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
