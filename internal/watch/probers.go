package watch

import (
	"context"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v4/process"
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

// ProcessProber: running = some process's full command line contains
// pattern as a case-sensitive substring. Implemented with gopsutil on
// every platform (owner decision, windows-support spec §2) — identical
// semantics on macOS/Linux/Windows, no pgrep dependency. The prober's
// own process (the daemon) is excluded; as with pgrep before it, a
// pattern matching another runtimepulse invocation's argv will match.
func ProcessProber(pattern string) Prober {
	self := int32(os.Getpid())
	return func(ctx context.Context) bool {
		procs, err := process.ProcessesWithContext(ctx)
		if err != nil {
			return false
		}
		for _, p := range procs {
			if p.Pid == self {
				continue
			}
			cmdline, err := p.CmdlineWithContext(ctx)
			if err != nil || cmdline == "" {
				continue // permission-denied or exited processes are not matches
			}
			if strings.Contains(cmdline, pattern) {
				return true
			}
		}
		return false
	}
}
