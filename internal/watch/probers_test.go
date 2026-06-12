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
	"time"

	"github.com/tufantunc/RuntimePulse/internal/mockexe"
)

// TestMain lets this test binary double as the marker child process
// for TestProcessProber (re-exec pattern; see internal/mockexe).
func TestMain(m *testing.M) {
	mockexe.Main()
	os.Exit(m.Run())
}

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
	// A real redirect must NOT be followed: target answering 302 with a
	// dead Location is still "available" (the target itself responded).
	redirecting := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://127.0.0.1:1/dead", http.StatusFound)
	}))
	defer redirecting.Close()
	if !HTTPProber(redirecting.URL)(ctx) {
		t.Fatal("redirect to dead target must still count as available")
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
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	marker := fmt.Sprintf("rp-marker-%d", os.Getpid())
	cmd := exec.Command(exe, marker) // marker lands in the child's cmdline
	cmd.Env = append(os.Environ(), mockexe.Env(mockexe.Spec{DelayMs: 30000})...)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { cmd.Process.Kill(); cmd.Wait() }()

	p := ProcessProber(marker)
	ctx := context.Background()
	// process tables refresh asynchronously on some platforms; poll briefly
	deadline := time.Now().Add(3 * time.Second)
	for !p(ctx) {
		if time.Now().After(deadline) {
			t.Fatal("running marker process must be found")
		}
		time.Sleep(50 * time.Millisecond)
	}

	cmd.Process.Kill()
	cmd.Wait()
	deadline = time.Now().Add(3 * time.Second)
	for p(ctx) {
		if time.Now().After(deadline) {
			t.Fatal("dead marker process must not be found")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestProcessProberExcludesSelf(t *testing.T) {
	// our own test-binary path is in our own cmdline; the prober must
	// not report US as a match for it
	self, _ := os.Executable()
	p := ProcessProber(self + " unique-never-spawned-suffix")
	if p(context.Background()) {
		t.Fatal("prober must not match a pattern only present in nonexistent processes")
	}
}
