package adapter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"strings"
	"time"
	"unicode/utf8"
)

const summaryLimit = 500

// runCLI executes an agent CLI in dir under supervision. Contract
// (shared by all adapters): a nonzero agent exit is a Result, not an
// error; ctx cancellation/timeout is annotated in the Result; only a
// spawn failure (missing binary, …) returns an error.
//
// Summary policy depends on outcome, because CLIs split their streams
// differently on success vs failure:
//   - success: the agent's answer is the parsed stdout; if stdout is
//     empty, fall back to a marked stderr tail.
//   - failure: the diagnostic lives on stderr (stdout is often
//     unrelated progress/plugin chatter), so surface stderr; if stderr
//     is empty, fall back to the parsed stdout (some CLIs error there).
func runCLI(ctx context.Context, bin string, args []string, dir string, parse func([]byte) string) (Result, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr

	start := time.Now()
	err := cmd.Run()
	res := Result{
		Command:    bin + " " + strings.Join(args, " "),
		DurationMs: time.Since(start).Milliseconds(),
	}
	errTail := truncate(strings.TrimSpace(stderr.String()))

	if err != nil {
		if ctx.Err() != nil {
			res.ExitCode = -1
			res.OutputSummary = "resume timed out or cancelled"
			if errTail != "" {
				res.OutputSummary += ": " + errTail
			}
			return res, nil
		}
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			res.ExitCode = ee.ExitCode()
			if errTail != "" {
				res.OutputSummary = errTail // the error, not stdout chatter
			} else {
				res.OutputSummary = parse(stdout.Bytes())
			}
			return res, nil
		}
		return res, err
	}

	// Success.
	res.OutputSummary = parse(stdout.Bytes())
	if res.OutputSummary == "" && errTail != "" {
		// Marked so a fallback summary is never mistaken for an agent
		// message on a successful run with empty stdout.
		res.OutputSummary = "(stderr) " + errTail
	}
	return res, nil
}

// ParseResultJSON extracts the "result" field from the JSON object that
// both Claude (`--output-format json`) and Cursor (`--output-format
// json`) print; falls back to the raw text. The boolean reports whether
// structured output was recognized.
func ParseResultJSON(out []byte) (string, bool) {
	var r struct {
		Result string `json:"result"`
	}
	if err := json.Unmarshal(out, &r); err == nil && r.Result != "" {
		return truncate(r.Result), true
	}
	return truncate(strings.TrimSpace(string(out))), false
}

func truncate(s string) string {
	if len(s) <= summaryLimit {
		return s
	}
	cut := summaryLimit
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}
