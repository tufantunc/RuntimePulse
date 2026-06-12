package client

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// EnsureDaemon returns a client for the daemon at dir, starting the
// daemon (detached, logging to daemon.log) if it is not running.
func EnsureDaemon(dir, socket string) (*Client, error) {
	c := New(socket)
	if c.ping() == nil {
		return c, nil
	}

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	logf, err := os.OpenFile(filepath.Join(dir, "daemon.log"),
		os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	defer logf.Close()

	cmd := exec.Command(exe, "daemon")
	cmd.Env = append(os.Environ(), "RUNTIMEPULSE_DIR="+dir)
	cmd.Stdout, cmd.Stderr = logf, logf
	cmd.SysProcAttr = detachSysProcAttr() // survive parent exit
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	cmd.Process.Release()

	for i := 0; i < 30; i++ {
		if c.ping() == nil {
			return c, nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return nil, errors.New("daemon did not start; see " + filepath.Join(dir, "daemon.log"))
}

func (c *Client) ping() error {
	var out map[string]any
	return c.Call("status", nil, &out)
}
