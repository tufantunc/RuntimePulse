//go:build !windows

package daemon

import "os"

// chmodSocket restricts the daemon socket to the owning user (spec §9).
func chmodSocket(path string) error { return os.Chmod(path, 0o600) }
