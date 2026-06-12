//go:build !windows

package daemon

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// acquireLock takes an exclusive, non-blocking flock on dir/daemon.lock
// for the daemon's lifetime. It serializes socket acquisition across
// concurrent autostarts: net.Listen("unix") is bind-then-listen, so an
// unguarded stale-socket probe could unlink a live daemon's socket in
// the window between the two. Released automatically on process death.
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
