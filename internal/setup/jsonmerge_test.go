package setup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("result is not valid JSON: %v\n%s", err, b)
	}
	return m
}

func TestMergeCreatesFileAndParents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "mcp.json")
	entry := map[string]any{"command": "/bin/rp", "args": []string{"mcp"}}
	if err := mergeJSONServer(path, "mcpServers", "runtimepulse", entry); err != nil {
		t.Fatal(err)
	}
	m := readJSON(t, path)
	servers := m["mcpServers"].(map[string]any)
	rp := servers["runtimepulse"].(map[string]any)
	if rp["command"] != "/bin/rp" {
		t.Fatalf("entry not written: %#v", rp)
	}
}

func TestMergePreservesExistingContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp.json")
	seed := `{"$schema":"https://x/config.json","mcpServers":{"other":{"command":"keep"}}}`
	if err := os.WriteFile(path, []byte(seed), 0o600); err != nil {
		t.Fatal(err)
	}
	entry := map[string]any{"command": "/bin/rp", "args": []string{"mcp"}}
	if err := mergeJSONServer(path, "mcpServers", "runtimepulse", entry); err != nil {
		t.Fatal(err)
	}
	m := readJSON(t, path)
	if m["$schema"] != "https://x/config.json" {
		t.Fatal("$schema lost")
	}
	servers := m["mcpServers"].(map[string]any)
	if servers["other"].(map[string]any)["command"] != "keep" {
		t.Fatal("existing server lost")
	}
	if servers["runtimepulse"] == nil {
		t.Fatal("runtimepulse not added")
	}
}

func TestMergeIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp.json")
	entry := map[string]any{"command": "/bin/rp", "args": []string{"mcp"}}
	for i := 0; i < 2; i++ {
		if err := mergeJSONServer(path, "mcp", "runtimepulse", entry); err != nil {
			t.Fatal(err)
		}
	}
	m := readJSON(t, path)
	mcp := m["mcp"].(map[string]any)
	if len(mcp) != 1 {
		t.Fatalf("re-run duplicated keys: %#v", mcp)
	}
}

func TestMergeRefusesUnparseableFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp.json")
	if err := os.WriteFile(path, []byte("{ not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := mergeJSONServer(path, "mcp", "runtimepulse", map[string]any{}); err == nil {
		t.Fatal("must refuse to overwrite an unparseable file")
	}
	// original untouched
	b, _ := os.ReadFile(path)
	if string(b) != "{ not json" {
		t.Fatalf("clobbered the bad file: %s", b)
	}
}

func TestMergePreservesLargeIntegers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp.json")
	seed := `{"mcp":{"other":{"port":1152921504606846976}}}`
	if err := os.WriteFile(path, []byte(seed), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := mergeJSONServer(path, "mcp", "runtimepulse", map[string]any{"command": "x"}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), "1152921504606846976") {
		t.Fatalf("large integer mangled: %s", b)
	}
}

func TestHasServer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp.json")
	if hasJSONServer(path, "mcp", "runtimepulse") {
		t.Fatal("absent file must report not-registered")
	}
	mergeJSONServer(path, "mcp", "runtimepulse", map[string]any{"command": "x"})
	if !hasJSONServer(path, "mcp", "runtimepulse") {
		t.Fatal("registered server must be detected")
	}
}
