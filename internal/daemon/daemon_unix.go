//go:build unix || linux || darwin

package daemon

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
		log.Printf("Failed to start daemon: %v", err)
		return fmt.Errorf("failed to start daemon: %w", err)
	}

	// Don't wait for the process
	go cmd.Wait()

	return nil
}

func processMatches(pid int, executable string) bool {
	actual, err := os.Readlink(filepath.Join("/proc", fmt.Sprint(pid), "exe"))
	if err != nil {
		// No /proc on this platform (e.g. macOS): identity can't be
		// verified, so fall back to the plain liveness check.
		if _, statErr := os.Stat("/proc"); statErr != nil {
			return true
		}
		return false
	}
	// The link gains a " (deleted)" suffix once the binary on disk is
	// replaced, e.g. after a rebuild of a still-running daemon.
	actual = strings.TrimSuffix(actual, " (deleted)")
	return actual == executable
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
		log.Printf("Failed to find process: %v", err)
		return fmt.Errorf("failed to find process: %w", err)
	}

	// Send SIGTERM
	if err := process.Signal(syscall.SIGTERM); err != nil {
		log.Printf("Failed to stop daemon: %v", err)
		return fmt.Errorf("failed to stop daemon: %w", err)
	}

	return nil
}
