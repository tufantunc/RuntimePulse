package setup

import "fmt"

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

// Result is the outcome of registering one agent.
type Result struct {
	Agent   string
	OK      bool
	Skipped bool   // already registered
	Note    string // e.g. scope fallback explanation
	Err     error
}

// AgentStatus pairs an agent with its detected status (for the wizard's
// detection table).
type AgentStatus struct {
	Agent  Agent
	Status Status
}

// Registry returns the four supported agents, wired for production
// (real home/cwd, real subprocess runner). home and cwd come from the
// environment; a caller may pass overrides for testing.
func Registry(home, cwd string) []Agent {
	return []Agent{
		claudeAgent{},
		cursorAgent{home: home, cwd: cwd},
		codexAgent{},
		openCodeAgent{home: home, cwd: cwd},
	}
}

// DetectAll inspects every agent at the given scope.
func DetectAll(agents []Agent, scope Scope) []AgentStatus {
	out := make([]AgentStatus, 0, len(agents))
	for _, a := range agents {
		out = append(out, AgentStatus{Agent: a, Status: a.Status(scope)})
	}
	return out
}

// Run registers the named, installed agents at scope. Each agent is
// independent: a failure is recorded, never fatal. Already-registered
// agents are skipped. An agent that doesn't support the requested scope
// falls back to user scope with an explanatory note.
func Run(agents []Agent, names []string, binPath string, scope Scope) []Result {
	selected := map[string]bool{}
	for _, n := range names {
		selected[n] = true
	}
	var results []Result
	for _, a := range agents {
		if !selected[a.Name()] {
			continue
		}
		res := Result{Agent: a.Name()}
		st := a.Status(scope)
		switch {
		case !st.Installed:
			res.Err = fmt.Errorf("%s CLI not found on PATH", a.Name())
		case st.Registered:
			res.OK, res.Skipped = true, true
		default:
			useScope := scope
			if !a.SupportsScope(scope) {
				useScope = ScopeUser
				res.Note = a.Name() + ": " + scope.String() + " scope unsupported, registered at user scope"
			}
			if err := a.Register(binPath, useScope); err != nil {
				res.Err = err
			} else {
				res.OK = true
			}
		}
		results = append(results, res)
	}
	return results
}
