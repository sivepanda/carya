//go:build unix || linux || darwin

package daemon

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

// startProcess starts the daemon process with Unix-specific attributes
func startProcess(cmd *exec.Cmd, logFile *os.File) error {
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true, // Create new session
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start daemon: %w", err)
	}

	// Don't wait for the process
	go cmd.Wait()

	return nil
}

// isProcessRunning checks if a process is running on Unix systems
func isProcessRunning(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}

	// Send signal 0 to check if process is alive
	err = process.Signal(syscall.Signal(0))
	return err == nil
}

// stopProcess stops the daemon process on Unix systems
func stopProcess(pid int) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("failed to find process: %w", err)
	}

	// Send SIGTERM
	if err := process.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("failed to stop daemon: %w", err)
	}

	return nil
}
