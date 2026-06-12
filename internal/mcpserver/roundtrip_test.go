package mcpserver

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestRoundTripListsToolsAndCreatesWatch drives the server through a real
// MCP client over the SDK's in-memory transport — proving tool
// registration, schema derivation, and dispatch end to end.
func TestRoundTripListsToolsAndCreatesWatch(t *testing.T) {
	c := newTestDaemonClient(t)
	srv := New(c)

	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	ctx := context.Background()

	go func() {
		if err := srv.Run(ctx, serverTransport); err != nil {
			t.Errorf("server.Run: %v", err)
		}
	}()

	mc := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "v0"}, nil)
	sess, err := mc.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()

	tools, err := sess.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"create_watch": false, "create_rule": false, "wait_for_event": false, "get_events": false, "cancel_rule": false}
	for _, tool := range tools.Tools {
		if _, ok := want[tool.Name]; ok {
			want[tool.Name] = true
		}
	}
	for name, seen := range want {
		if !seen {
			t.Fatalf("tool %q not advertised", name)
		}
	}

	res, err := sess.CallTool(ctx, &mcp.CallToolParams{
		Name:      "create_watch",
		Arguments: map[string]any{"type": "tcp", "target": "localhost:5432", "interval": "1s"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("create_watch returned tool error: %+v", res.Content)
	}
}
