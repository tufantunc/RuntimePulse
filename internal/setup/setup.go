package setup

// Scope selects user/global vs current-project registration.
type Scope int

const (
	ScopeUser Scope = iota
	ScopeProject
)

func (s Scope) String() string {
	if s == ScopeProject {
		return "project"
	}
	return "user"
}

// Status is the result of inspecting one agent.
type Status struct {
	Installed  bool
	Registered bool
	Detail     string
}

// Agent registers RuntimePulse as an MCP server for one agent CLI.
type Agent interface {
	Name() string
	Status(scope Scope) Status
	Register(binPath string, scope Scope) error
	SupportsScope(scope Scope) bool
}
