//go:build windows

package client

import (
	"syscall"

	"golang.org/x/sys/windows"
)

// detachSysProcAttr makes the autostarted daemon survive parent exit.
func detachSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		CreationFlags: windows.DETACHED_PROCESS | windows.CREATE_NEW_PROCESS_GROUP,
	}
}
