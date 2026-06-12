package setup

import (
	"path/filepath"
	"testing"
)

func TestCursorRegisterUserScope(t *testing.T) {
	home := t.TempDir()
	a := cursorAgent{home: home, cwd: t.TempDir()}
	if !a.SupportsScope(ScopeProject) || !a.SupportsScope(ScopeUser) {
		t.Fatal("cursor supports both scopes")
	}
	if err := a.Register("/abs/runtimepulse", ScopeUser); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".cursor", "mcp.json")
	m := readJSON(t, path)
	rp := m["mcpServers"].(map[string]any)["runtimepulse"].(map[string]any)
	if rp["command"] != "/abs/runtimepulse" {
		t.Fatalf("bad command: %#v", rp)
	}
	args := rp["args"].([]any)
	if len(args) != 1 || args[0] != "mcp" {
		t.Fatalf("bad args: %#v", args)
	}
	if !a.Status(ScopeUser).Registered {
		t.Fatal("Status must report registered after Register")
	}
}

func TestCursorRegisterProjectScope(t *testing.T) {
	cwd := t.TempDir()
	a := cursorAgent{home: t.TempDir(), cwd: cwd}
	if err := a.Register("/abs/runtimepulse", ScopeProject); err != nil {
		t.Fatal(err)
	}
	readJSON(t, filepath.Join(cwd, ".cursor", "mcp.json")) // exists & valid
}

func TestOpenCodeRegisterShape(t *testing.T) {
	cwd := t.TempDir()
	a := openCodeAgent{home: t.TempDir(), cwd: cwd}
	if err := a.Register("/abs/runtimepulse", ScopeProject); err != nil {
		t.Fatal(err)
	}
	m := readJSON(t, filepath.Join(cwd, "opencode.json"))
	rp := m["mcp"].(map[string]any)["runtimepulse"].(map[string]any)
	if rp["type"] != "local" || rp["enabled"] != true {
		t.Fatalf("bad opencode entry: %#v", rp)
	}
	cmd := rp["command"].([]any)
	if len(cmd) != 2 || cmd[0] != "/abs/runtimepulse" || cmd[1] != "mcp" {
		t.Fatalf("bad command array: %#v", cmd)
	}
}
