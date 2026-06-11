package watch

import (
	"context"
	"net"
	"net/http"
	"os/exec"
	"time"
)

const probeTimeout = 2 * time.Second

// HTTPProber: available = the target ITSELF answers with a 2xx/3xx
// status. Redirects are not followed — an app that 302s "/" → "/login"
// is up; the redirect target's health is not this watch's business.
func HTTPProber(url string) Prober {
	client := &http.Client{
		Timeout: probeTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	return func(ctx context.Context) bool {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return false
		}
		resp, err := client.Do(req)
		if err != nil {
			return false
		}
		// Close without draining: the connection is not reused, which is
		// deliberate — each probe is a fresh, honest liveness check.
		resp.Body.Close()
		return resp.StatusCode >= 200 && resp.StatusCode < 400
	}
}

// TCPProber: available = the address accepts a TCP connection.
// This is the service-agnostic readiness check (postgres, redis, …).
func TCPProber(addr string) Prober {
	return func(ctx context.Context) bool {
		d := net.Dialer{Timeout: probeTimeout}
		conn, err := d.DialContext(ctx, "tcp", addr)
		if err != nil {
			return false
		}
		conn.Close()
		return true
	}
}

// ProcessProber: running = `pgrep -f pattern` finds a process.
// pgrep excludes itself; the daemon's own argv only matches if the
// pattern happens to match "runtimepulse daemon".
func ProcessProber(pattern string) Prober {
	return func(ctx context.Context) bool {
		return exec.CommandContext(ctx, "pgrep", "-f", pattern).Run() == nil
	}
}
