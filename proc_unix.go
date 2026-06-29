//go:build !windows

package main

import (
	"os/exec"
	"syscall"
)

// setProcAttr puts the child in its own process group so the whole tree can be
// signalled on cancel / timeout.
func setProcAttr(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killProcessGroup sends SIGKILL to the child's process group.
func killProcessGroup(c *exec.Cmd) {
	if c.Process == nil {
		return
	}
	pgid, err := syscall.Getpgid(c.Process.Pid)
	if err == nil {
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
		return
	}
	_ = c.Process.Kill()
}
