package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"time"

	"github.com/spf13/cobra"
)

// exec runs a command and converts its outcome into an event
// (exec.succeeded / exec.failed, source = label). This is how builds,
// migrations and `docker compose up --wait` enter the event stream.
func execCmd() *cobra.Command {
	var label string
	cmd := &cobra.Command{
		Use:   "exec --label <name> -- <command> [args...]",
		Short: "Run a command and emit exec.succeeded/failed",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			start := time.Now()
			child := exec.Command(args[0], args[1:]...)
			child.Stdin, child.Stdout, child.Stderr = os.Stdin, os.Stdout, os.Stderr
			runErr := child.Run()

			exitCode := 0
			evType := "exec.succeeded"
			if runErr != nil {
				evType = "exec.failed"
				exitCode = 1
				var ee *exec.ExitError
				if errors.As(runErr, &ee) {
					exitCode = ee.ExitCode()
				}
			}
			c, err := dial()
			if err != nil {
				return fmt.Errorf("command finished (exit %d) but daemon unreachable: %w", exitCode, err)
			}
			var res map[string]any
			if err := c.Call("event.inject", map[string]any{
				"type": evType, "source": label,
				"payload": map[string]string{
					"exitCode":   strconv.Itoa(exitCode),
					"durationMs": strconv.FormatInt(time.Since(start).Milliseconds(), 10),
				},
			}, &res); err != nil {
				return err
			}
			printJSON(res)
			if exitCode != 0 {
				os.Exit(exitCode) // preserve the child's exit code for shell chaining
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&label, "label", "", "event source label (required)")
	cmd.MarkFlagRequired("label")
	return cmd
}
