package mockexe

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	Main()
	os.Exit(m.Run())
}

func TestChildBehavesPerSpec(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "ignored", "- dash last arg")
	cmd.Env = append(os.Environ(), Env(Spec{Stdout: `{"result":"{LAST}"}`, Exit: 0})...)
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "- dash last arg") {
		t.Fatalf("LAST expansion failed: %s", out)
	}

	cmd = exec.Command(exe)
	cmd.Env = append(os.Environ(), Env(Spec{Stderr: "boom", Exit: 3})...)
	out2, err := cmd.CombinedOutput()
	var ee *exec.ExitError
	if err == nil || !errAs(err, &ee) || ee.ExitCode() != 3 {
		t.Fatalf("exit code not honored: %v", err)
	}
	if !strings.Contains(string(out2), "boom") {
		t.Fatalf("stderr missing: %s", out2)
	}
}

func errAs(err error, target **exec.ExitError) bool {
	e, ok := err.(*exec.ExitError)
	if ok {
		*target = e
	}
	return ok
}
