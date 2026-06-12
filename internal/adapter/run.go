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
// spawn failure (missing binary, …) returns an error. The parser sees
// stdout only — progress chatter on stderr never pollutes the summary,
// but becomes the fallback summary when stdout yields nothing.
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
	res.OutputSummary = parse(stdout.Bytes())
	if res.OutputSummary == "" {
		res.OutputSummary = truncate(strings.TrimSpace(stderr.String()))
	}

	if err != nil {
		if ctx.Err() != nil {
			res.ExitCode = -1
			res.OutputSummary = "resume timed out or cancelled: " + res.OutputSummary
			return res, nil
		}
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			res.ExitCode = ee.ExitCode()
			return res, nil
		}
		return res, err
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
