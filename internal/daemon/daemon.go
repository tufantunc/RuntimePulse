// Package daemon hosts the engine behind a unix-socket RPC server.
package daemon

import (
	"context"
	"net"
	"os"
	"time"

	"github.com/tufantunc/RuntimePulse/internal/bus"
	"github.com/tufantunc/RuntimePulse/internal/engine"
	"github.com/tufantunc/RuntimePulse/internal/store"
)

const Version = "0.1.0-dev"

type Daemon struct {
	Dir    string
	Engine *engine.Engine

	store *store.Store
	bus   *bus.Bus
	ln    net.Listener
}

func New(dir string) (*Daemon, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	st, err := store.Open(DBPath(dir))
	if err != nil {
		return nil, err
	}
	b := bus.New()

	sock := SocketPath(dir)
	removeStaleSocket(sock)
	ln, err := net.Listen("unix", sock)
	if err != nil {
		st.Close()
		return nil, err // includes "address already in use" → daemon already running
	}
	if err := os.Chmod(sock, 0o600); err != nil {
		ln.Close()
		st.Close()
		return nil, err
	}
	return &Daemon{Dir: dir, Engine: engine.New(st, b), store: st, bus: b, ln: ln}, nil
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
	d.ln.Close()
	d.store.Close()
	os.Remove(SocketPath(d.Dir))
}
