package daemon

import (
	"os"
	"path/filepath"
)

// StateDir resolves the daemon's state directory:
// $RUNTIMEPULSE_DIR if set, else ~/.runtimepulse.
func StateDir() (string, error) {
	if d := os.Getenv("RUNTIMEPULSE_DIR"); d != "" {
		return d, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".runtimepulse"), nil
}

func SocketPath(dir string) string { return filepath.Join(dir, "daemon.sock") }
func DBPath(dir string) string     { return filepath.Join(dir, "runtimepulse.db") }
func LogPath(dir string) string    { return filepath.Join(dir, "daemon.log") }
