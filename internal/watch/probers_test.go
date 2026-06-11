package watch

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"sync/atomic"
	"testing"
)

func TestHTTPProber(t *testing.T) {
	var code atomic.Int32
	code.Store(200)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(int(code.Load()))
	}))
	defer srv.Close()

	p := HTTPProber(srv.URL)
	ctx := context.Background()
	if !p(ctx) {
		t.Fatal("200 must be available")
	}
	code.Store(302)
	if !p(ctx) {
		t.Fatal("3xx must be available")
	}
	code.Store(500)
	if p(ctx) {
		t.Fatal("5xx must be unavailable")
	}
	srv.Close()
	if p(ctx) {
		t.Fatal("connection refused must be unavailable")
	}
}

func TestTCPProber(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	p := TCPProber(addr)
	ctx := context.Background()
	if !p(ctx) {
		t.Fatal("listening port must be available")
	}
	ln.Close()
	if p(ctx) {
		t.Fatal("closed port must be unavailable")
	}
}

func TestProcessProber(t *testing.T) {
	if _, err := exec.LookPath("pgrep"); err != nil {
		t.Skip("pgrep not available")
	}
	marker := fmt.Sprintf("31.41592%d", os.Getpid())
	cmd := exec.Command("sleep", marker)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { cmd.Process.Kill(); cmd.Wait() }()

	p := ProcessProber("sleep " + marker)
	ctx := context.Background()
	if !p(ctx) {
		t.Fatal("running process must be found")
	}
	cmd.Process.Kill()
	cmd.Wait()
	if p(ctx) {
		t.Fatal("dead process must not be found")
	}
}
