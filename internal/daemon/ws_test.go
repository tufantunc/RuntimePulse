package daemon

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestServeWSTokenFileAndAuth(t *testing.T) {
	dir := shortTempDir(t)
	d, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); d.Close() })
	go d.Serve(ctx)

	addr, err := d.ServeWS(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}

	tokenBytes, err := os.ReadFile(filepath.Join(dir, "ws-token"))
	if err != nil {
		t.Fatal(err)
	}
	token := strings.TrimSpace(string(tokenBytes))
	if len(token) < 16 {
		t.Fatalf("token too short: %q", token)
	}
	info, _ := os.Stat(filepath.Join(dir, "ws-token"))
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("token file perm = %o, want 0600", perm)
	}

	resp, err := http.Get("http://" + addr + "/events")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no token must 401, got %d", resp.StatusCode)
	}
	// with the right token but no websocket upgrade: not 401 (auth passed)
	resp, err = http.Get("http://" + addr + "/events?token=" + token)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		t.Fatal("valid token must pass auth")
	}

	// token persists across calls
	d2addr, err := d.ServeWS(ctx, 0)
	_ = d2addr
	if err == nil {
		t.Log("second ServeWS allowed (separate listener) — acceptable")
	}
	tokenBytes2, _ := os.ReadFile(filepath.Join(dir, "ws-token"))
	if string(tokenBytes) != string(tokenBytes2) {
		t.Fatal("token must persist, not regenerate")
	}
}
