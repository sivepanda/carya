//go:build unix || linux || darwin

package daemon

import (
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
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

	// The caller waits on cmd (see confirmStarted) so the child is reaped.
	return nil
}

// AcquireLock takes the exclusive per-repository daemon lock, which the kernel
// releases when the process exits (even on SIGKILL). It returns
// ErrAlreadyRunning if another daemon holds the lock for longer than wait;
// the short retry window keeps a momentary holder from causing a false
// "already running". The returned file must stay open, and referenced, for
// as long as the daemon runs.
func (d *Daemon) AcquireLock(wait time.Duration) (*os.File, error) {
	f, err := os.OpenFile(d.lockFile, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, fmt.Errorf("open daemon lock: %w", err)
	}

	deadline := time.Now().Add(wait)
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return f, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			f.Close()
			return nil, fmt.Errorf("lock %s: %w", d.lockFile, err)
		}
		if time.Now().After(deadline) {
			f.Close()
			return nil, ErrAlreadyRunning
		}
		time.Sleep(50 * time.Millisecond)
	}
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
