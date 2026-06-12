package setup

import "testing"

// fakeAgent lets the orchestration tests avoid real CLIs/filesystem.
type fakeAgent struct {
	name       string
	installed  bool
	registered bool
	supportPrj bool
	regErr     error
	regCalls   int
}

func (f *fakeAgent) Name() string { return f.name }
func (f *fakeAgent) Status(Scope) Status {
	return Status{Installed: f.installed, Registered: f.registered}
}
func (f *fakeAgent) SupportsScope(s Scope) bool { return s == ScopeUser || f.supportPrj }
func (f *fakeAgent) Register(string, Scope) error {
	f.regCalls++
	return f.regErr
}

func TestRunRegistersOnlySelectedInstalled(t *testing.T) {
	claude := &fakeAgent{name: "claude", installed: true}
	cursor := &fakeAgent{name: "cursor", installed: false}
	agents := []Agent{claude, cursor}

	results := Run(agents, []string{"claude", "cursor"}, "/bin/rp", ScopeUser)

	if claude.regCalls != 1 {
		t.Fatalf("installed selected agent must be registered once, got %d", claude.regCalls)
	}
	if cursor.regCalls != 0 {
		t.Fatal("uninstalled agent must not be registered")
	}
	if len(results) != 2 {
		t.Fatalf("a result per selected agent, got %d", len(results))
	}
}

func TestRunReportsFailureIndependently(t *testing.T) {
	ok := &fakeAgent{name: "claude", installed: true}
	bad := &fakeAgent{name: "opencode", installed: true, regErr: errBoom}
	results := Run([]Agent{ok, bad}, []string{"claude", "opencode"}, "/bin/rp", ScopeUser)

	var okRes, badRes *Result
	for i := range results {
		switch results[i].Agent {
		case "claude":
			okRes = &results[i]
		case "opencode":
			badRes = &results[i]
		}
	}
	if okRes == nil || !okRes.OK {
		t.Fatal("good agent must succeed despite the other's failure")
	}
	if badRes == nil || badRes.OK || badRes.Err == nil {
		t.Fatal("failing agent must be reported, not abort the run")
	}
}

func TestRunSkipsAlreadyRegistered(t *testing.T) {
	a := &fakeAgent{name: "claude", installed: true, registered: true}
	results := Run([]Agent{a}, []string{"claude"}, "/bin/rp", ScopeUser)
	if a.regCalls != 0 {
		t.Fatal("already-registered agent must not be re-registered")
	}
	if !results[0].Skipped {
		t.Fatal("already-registered must be reported as skipped")
	}
}

func TestRunUnsupportedScopeFallsBackToUser(t *testing.T) {
	codex := &fakeAgent{name: "codex", installed: true, supportPrj: false}
	results := Run([]Agent{codex}, []string{"codex"}, "/bin/rp", ScopeProject)
	if codex.regCalls != 1 {
		t.Fatal("codex must still register at user scope")
	}
	if results[0].Note == "" {
		t.Fatal("a note must explain the scope fallback")
	}
}

var errBoom = &boomErr{}

type boomErr struct{}

func (*boomErr) Error() string { return "boom" }
