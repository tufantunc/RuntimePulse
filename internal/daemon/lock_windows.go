//go:build windows

package daemon

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// acquireLock is the Windows counterpart of the unix flock: an
// exclusive, fail-immediately LockFileEx on dir/daemon.lock, held for
// the daemon's lifetime and released automatically on process death.
func acquireLock(dir string) (*os.File, error) {
	f, err := os.OpenFile(filepath.Join(dir, "daemon.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	ol := new(windows.Overlapped)
	if err := windows.LockFileEx(windows.Handle(f.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0, 1, 0, ol); err != nil {
		f.Close()
		return nil, fmt.Errorf("daemon already starting or running (lock held): %w", err)
	}
	return f, nil
}
