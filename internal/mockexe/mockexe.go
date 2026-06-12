// Package mockexe lets a test binary impersonate an agent CLI: call
// Main() first in TestMain; in child processes spawned with Env(spec)
// it performs the configured behavior and exits. This replaces
// sh mock scripts so adapter/daemon tests run on Windows.
package mockexe

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const sentinel = "RUNTIMEPULSE_TEST_MOCK"

// Spec describes the mocked CLI's behavior. In Stdout/Stderr the token
// {LAST} is replaced with the process's last argv element — that is how
// dash-prompt tests prove the prompt arrived intact.
type Spec struct {
	Stdout  string
	Stderr  string
	Exit    int
	DelayMs int
}

// Env returns the environment entries that make a child running the
// test binary behave per spec. Append to os.Environ() — or set them via
// t.Setenv so children inherit them implicitly.
func Env(s Spec) []string {
	return []string{
		sentinel + "=1",
		"MOCK_STDOUT=" + s.Stdout,
		"MOCK_STDERR=" + s.Stderr,
		"MOCK_EXIT=" + strconv.Itoa(s.Exit),
		"MOCK_DELAY_MS=" + strconv.Itoa(s.DelayMs),
	}
}

// Main must be the first call in TestMain. No-op in the test process;
// in a child spawned with Env(spec) it acts as the mock and never
// returns.
func Main() {
	if os.Getenv(sentinel) != "1" {
		return
	}
	if d, _ := strconv.Atoi(os.Getenv("MOCK_DELAY_MS")); d > 0 {
		time.Sleep(time.Duration(d) * time.Millisecond)
	}
	last := ""
	if len(os.Args) > 1 {
		last = os.Args[len(os.Args)-1]
	}
	expand := func(s string) string { return strings.ReplaceAll(s, "{LAST}", last) }
	if out := os.Getenv("MOCK_STDOUT"); out != "" {
		fmt.Print(expand(out))
	}
	if errOut := os.Getenv("MOCK_STDERR"); errOut != "" {
		fmt.Fprint(os.Stderr, expand(errOut))
	}
	code, _ := strconv.Atoi(os.Getenv("MOCK_EXIT"))
	os.Exit(code)
}
