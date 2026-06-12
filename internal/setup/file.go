package setup

import (
	"os/exec"
	"path/filepath"
)

// --- Cursor: ~/.cursor/mcp.json (user) or <cwd>/.cursor/mcp.json (project) ---

type cursorAgent struct {
	home string // injected in tests; production uses os.UserHomeDir
	cwd  string
}

func (cursorAgent) Name() string { return "cursor" }

func (cursorAgent) SupportsScope(Scope) bool { return true }

func (a cursorAgent) installed() bool {
	if _, err := exec.LookPath("agent"); err == nil {
		return true
	}
	_, err := exec.LookPath("cursor-agent") // pre-rename installs
	return err == nil
}

func (a cursorAgent) path(scope Scope) string {
	if scope == ScopeProject {
		return filepath.Join(a.cwd, ".cursor", "mcp.json")
	}
	return filepath.Join(a.home, ".cursor", "mcp.json")
}

func (a cursorAgent) Status(scope Scope) Status {
	return Status{
		Installed:  a.installed(),
		Registered: hasJSONServer(a.path(scope), "mcpServers", "runtimepulse"),
	}
}

func (a cursorAgent) Register(binPath string, scope Scope) error {
	return mergeJSONServer(a.path(scope), "mcpServers", "runtimepulse",
		map[string]any{"command": binPath, "args": []string{"mcp"}})
}

// --- OpenCode: global config (user) or <cwd>/opencode.json (project) ---

type openCodeAgent struct {
	home string
	cwd  string
}

func (openCodeAgent) Name() string { return "opencode" }

func (openCodeAgent) SupportsScope(Scope) bool { return true }

func (a openCodeAgent) installed() bool {
	_, err := exec.LookPath("opencode")
	return err == nil
}

func (a openCodeAgent) path(scope Scope) string {
	if scope == ScopeProject {
		return filepath.Join(a.cwd, "opencode.json")
	}
	// Verified against opencode.ai/docs/config: ~/.config/opencode/opencode.json
	// is the documented XDG location on macOS/Linux.
	return filepath.Join(a.home, ".config", "opencode", "opencode.json")
}

func (a openCodeAgent) Status(scope Scope) Status {
	return Status{
		Installed:  a.installed(),
		Registered: hasJSONServer(a.path(scope), "mcp", "runtimepulse"),
	}
}

func (a openCodeAgent) Register(binPath string, scope Scope) error {
	return mergeJSONServer(a.path(scope), "mcp", "runtimepulse",
		map[string]any{"type": "local", "command": []string{binPath, "mcp"}, "enabled": true})
}
