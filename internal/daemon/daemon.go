// Package daemon hosts the engine behind a unix-socket RPC server.
package daemon

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/tufantunc/RuntimePulse/internal/adapter"
	"github.com/tufantunc/RuntimePulse/internal/bus"
	"github.com/tufantunc/RuntimePulse/internal/core"
	"github.com/tufantunc/RuntimePulse/internal/dispatch"
	"github.com/tufantunc/RuntimePulse/internal/engine"
	"github.com/tufantunc/RuntimePulse/internal/store"
	"github.com/tufantunc/RuntimePulse/internal/watch"
	"github.com/tufantunc/RuntimePulse/internal/wsserver"
)

const Version = "0.1.0-dev"

type Daemon struct {
	Dir      string
	Engine   *engine.Engine
	Watches  *watch.Manager
	Dispatch *dispatch.Dispatcher

	store *store.Store
	bus   *bus.Bus
	ln    net.Listener
	lock  *os.File
	// dispatchDone is allocated in New (immutable thereafter — no race
	// with Close) and closed by Serve's dispatch goroutine. Close waits
	// on it ONLY if that goroutine was actually launched: a Daemon that
	// was New()ed but never Serve()d (early CLI errors, tests) must not
	// stall 10s on a channel nobody will close.
	dispatchDone    chan struct{}
	dispatchStarted atomic.Bool
}

func New(dir string) (*Daemon, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	lock, err := acquireLock(dir)
	if err != nil {
		return nil, err
	}
	st, err := store.Open(DBPath(dir))
	if err != nil {
		lock.Close()
		return nil, err
	}
	b := bus.New()

	sock := SocketPath(dir)
	removeStaleSocket(sock)
	ln, err := net.Listen("unix", sock)
	if err != nil {
		st.Close()
		lock.Close()
		return nil, err // includes "address already in use" → daemon already running
	}
	if err := chmodSocket(sock); err != nil {
		ln.Close()
		st.Close()
		lock.Close()
		return nil, err
	}
	eng := engine.New(st, b)
	mgr := watch.NewManager(func(evType, source string, payload map[string]string) {
		// Watcher observations enter the same pipeline as injected events.
		// A persistence failure here is the durable-store ethos breaking;
		// it must at least be visible in the daemon log.
		if _, err := eng.Ingest(core.Event{Type: evType, Source: source, Payload: payload}); err != nil {
			log.Printf("watch emit: ingest %s from %s failed: %v", evType, source, err)
		}
	})
	disp := dispatch.New(st, adapter.Registry{
		"claude":   adapter.Claude{},
		"cursor":   adapter.Cursor{},
		"codex":    adapter.Codex{},
		"opencode": adapter.OpenCode{},
	}, eng.Ingest)
	eng.Notify = disp.Wake
	return &Daemon{Dir: dir, Engine: eng, Watches: mgr, Dispatch: disp, store: st, bus: b, ln: ln, lock: lock, dispatchDone: make(chan struct{})}, nil
}

// removeStaleSocket deletes a socket file nobody is listening on.
func removeStaleSocket(path string) {
	if _, err := os.Stat(path); err != nil {
		return
	}
	conn, err := net.DialTimeout("unix", path, 200*time.Millisecond)
	if err != nil {
		os.Remove(path)
		return
	}
	conn.Close() // live daemon; Listen will fail loudly
}

// Serve accepts connections until ctx is cancelled.
func (d *Daemon) Serve(ctx context.Context) error {
	go func() {
		<-ctx.Done()
		d.ln.Close()
	}()
	persisted, err := d.store.ListWatches()
	if err != nil {
		return err
	}
	d.Watches.Run(ctx, persisted)
	d.dispatchStarted.Store(true)
	go func() {
		d.Dispatch.Run(ctx)
		close(d.dispatchDone)
	}()
	for {
		conn, err := d.ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		go d.handleConn(conn)
	}
}

func (d *Daemon) Close() {
	if d.dispatchStarted.Load() {
		select {
		case <-d.dispatchDone:
		case <-time.After(10 * time.Second): // agent kill should be near-instant; don't hang forever
		}
	}
	d.ln.Close()
	d.store.Close()
	os.Remove(SocketPath(d.Dir))
	d.lock.Close()
}

// ServeWS starts the optional WebSocket event stream (spec §9: opt-in,
// loopback-only, token-gated). The token persists in <dir>/ws-token
// (0600) so external consumers can read it across daemon restarts.
func (d *Daemon) ServeWS(ctx context.Context, port int) (string, error) {
	token, err := loadOrCreateToken(filepath.Join(d.Dir, "ws-token"))
	if err != nil {
		return "", err
	}
	addr, err := wsserver.Start(ctx, port, token, d.bus)
	if err != nil {
		return "", err
	}
	log.Printf("ws: streaming events on ws://%s/events (token: %s/ws-token)", addr, d.Dir)
	return addr, nil
}

func loadOrCreateToken(path string) (string, error) {
	if data, err := os.ReadFile(path); err == nil {
		if tok := strings.TrimSpace(string(data)); tok != "" {
			return tok, nil
		}
	}
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	tok := hex.EncodeToString(raw)
	if err := os.WriteFile(path, []byte(tok+"\n"), 0o600); err != nil {
		return "", err
	}
	return tok, nil
}
