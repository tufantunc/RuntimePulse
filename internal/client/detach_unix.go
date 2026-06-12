//go:build !windows

package client

import "syscall"

// detachSysProcAttr makes the autostarted daemon survive parent exit.
func detachSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}
