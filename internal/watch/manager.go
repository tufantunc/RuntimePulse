package watch

import (
	"context"
	"fmt"
	"sort"
	"sync"
)

// Manager owns one goroutine per active watch inside the daemon.
type Manager struct {
	emit Emitter

	mu      sync.Mutex
	ctx     context.Context
	running map[string]*handle
}

// handle is a per-spawn token: the goroutine's deferred cleanup deletes
// its map entry only if the entry still holds ITS handle, so a
// Stop+Start of the same id can never have the old goroutine's cleanup
// orphan the new watcher.
type handle struct {
	cancel context.CancelFunc
}

func NewManager(emit Emitter) *Manager {
	return &Manager{emit: emit, running: map[string]*handle{}}
}

// Run records the lifetime ctx and arms all persisted watches
// (daemon boot re-arm). Must be called once before Start.
func (m *Manager) Run(ctx context.Context, persisted []Watch) {
	m.mu.Lock()
	m.ctx = ctx
	m.mu.Unlock()
	for _, w := range persisted {
		m.Start(w) // best-effort; an invalid persisted watch must not block boot
	}
}

// Start spawns the watcher for w; starting an already-running id is a no-op.
func (m *Manager) Start(w Watch) error {
	if err := ValidateType(w.Type); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ctx == nil {
		return fmt.Errorf("manager not running")
	}
	if _, ok := m.running[w.ID]; ok {
		return nil
	}
	ctx, cancel := context.WithCancel(m.ctx)
	h := &handle{cancel: cancel}
	m.running[w.ID] = h
	go func() {
		defer func() {
			m.mu.Lock()
			if m.running[w.ID] == h {
				delete(m.running, w.ID)
			}
			m.mu.Unlock()
		}()
		m.run(ctx, w)
	}()
	return nil
}

func (m *Manager) run(ctx context.Context, w Watch) {
	wrappedEmit := func(evType, source string, payload map[string]string) {
		if ctx.Err() != nil {
			return
		}
		m.emit(evType, source, payload)
	}
	switch w.Type {
	case "http":
		RunPoller(ctx, w.Config, "http.available", "http.unavailable", w.Target, HTTPProber(w.Target), wrappedEmit)
	case "tcp":
		RunPoller(ctx, w.Config, "tcp.available", "tcp.unavailable", w.Target, TCPProber(w.Target), wrappedEmit)
	case "process":
		RunPoller(ctx, w.Config, "process.started", "process.exited", w.Target, ProcessProber(w.Target), wrappedEmit)
	case "file":
		RunFileWatch(ctx, w.Config, w.Target, wrappedEmit)
	case "git":
		RunGitWatch(ctx, w.Config, w.Target, wrappedEmit)
	case "docker":
		RunDockerWatch(ctx, w.Config, w.Target, wrappedEmit)
	}
}

// Stop cancels the watcher for id (no-op if not running).
func (m *Manager) Stop(id string) {
	m.mu.Lock()
	h, ok := m.running[id]
	if ok {
		delete(m.running, id)
	}
	m.mu.Unlock()
	if ok {
		h.cancel()
	}
}

// RunningIDs returns the ids of currently running watchers, sorted.
func (m *Manager) RunningIDs() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, 0, len(m.running))
	for id := range m.running {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}
