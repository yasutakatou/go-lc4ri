//go:build windows

package main

import "os/exec"

// setProcAttr is a no-op on Windows (no process groups via Setpgid).
func setProcAttr(c *exec.Cmd) {}

// killProcessGroup kills the child process on Windows.
func killProcessGroup(c *exec.Cmd) {
	if c.Process != nil {
		_ = c.Process.Kill()
	}
}
