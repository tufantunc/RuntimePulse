// Package daemon hosts the engine behind a unix-socket RPC server.
package daemon

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/unix"

	"github.com/tufantunc/RuntimePulse/internal/adapter"
	"github.com/tufantunc/RuntimePulse/internal/bus"
	"github.com/tufantunc/RuntimePulse/internal/core"
	"github.com/tufantunc/RuntimePulse/internal/dispatch"
	"github.com/tufantunc/RuntimePulse/internal/engine"
	"github.com/tufantunc/RuntimePulse/internal/store"
	"github.com/tufantunc/RuntimePulse/internal/watch"
)

const Version = "0.1.0-dev"

type Daemon struct {
	Dir      string
	Engine   *engine.Engine
	Watches  *watch.Manager
	Dispatch *dispatch.Dispatcher

	store        *store.Store
	bus          *bus.Bus
	ln           net.Listener
	lock         *os.File
	dispatchDone chan struct{}
}

// acquireLock takes an exclusive, non-blocking flock on dir/daemon.lock
// for the daemon's lifetime. It serializes socket acquisition across
// concurrent autostarts: net.Listen("unix") is bind-then-listen, so an
// unguarded stale-socket probe could unlink a live daemon's socket in
// the window between the two.
func acquireLock(dir string) (*os.File, error) {
	f, err := os.OpenFile(filepath.Join(dir, "daemon.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf("daemon already starting or running (lock held): %w", err)
	}
	return f, nil
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
	if err := os.Chmod(sock, 0o600); err != nil {
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
	disp := dispatch.New(st, adapter.Registry{"claude": adapter.Claude{}}, eng.Ingest)
	eng.Notify = disp.Wake
	return &Daemon{Dir: dir, Engine: eng, Watches: mgr, Dispatch: disp, store: st, bus: b, ln: ln, lock: lock}, nil
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
	d.dispatchDone = make(chan struct{})
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
	if d.dispatchDone != nil {
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
