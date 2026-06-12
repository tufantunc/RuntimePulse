package mcpserver

import (
	"testing"

	"github.com/tufantunc/RuntimePulse/internal/client"
)

func TestNewBuildsServer(t *testing.T) {
	srv := New(client.New("/nonexistent.sock"))
	if srv == nil {
		t.Fatal("New must return a server")
	}
}
