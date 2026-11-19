//go:build unix

package main

import (
	"os/exec"
	"syscall"
)

// detach starts c in its own session so it outlives the hook that started it.
func detach(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
