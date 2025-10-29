package daemon

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// Daemon manages a background process with PID file
type Daemon struct {
	pidFile string
	logFile string
}

// New creates a new daemon manager
func New(pidFile, logFile string) *Daemon {
	return &Daemon{
		pidFile: pidFile,
		logFile: logFile,
	}
}

// IsRunning checks if the daemon is currently running
func (d *Daemon) IsRunning() bool {
	pid, err := d.ReadPID()
	if err != nil {
		return false
	}

	// Check if process exists
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}

	// Send signal 0 to check if process is alive
	err = process.Signal(syscall.Signal(0))
	return err == nil
}

// ReadPID reads the PID from the PID file
func (d *Daemon) ReadPID() (int, error) {
	data, err := os.ReadFile(d.pidFile)
	if err != nil {
		return 0, err
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, fmt.Errorf("invalid PID in file: %w", err)
	}

	return pid, nil
}

// WritePID writes the current process PID to the PID file
func (d *Daemon) WritePID() error {
	pid := os.Getpid()
	return os.WriteFile(d.pidFile, []byte(fmt.Sprintf("%d\n", pid)), 0644)
}

// RemovePID removes the PID file
func (d *Daemon) RemovePID() error {
	return os.Remove(d.pidFile)
}

// Start starts the daemon in background mode
func (d *Daemon) Start(args []string) error {
	if d.IsRunning() {
		return fmt.Errorf("daemon is already running")
	}

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(d.pidFile), 0755); err != nil {
		return fmt.Errorf("failed to create daemon directory: %w", err)
	}

	// Create log file
	logFile, err := os.OpenFile(d.logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("failed to create log file: %w", err)
	}
	defer logFile.Close()

	// Get current executable
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	// Start the process in background
	cmd := exec.Command(executable, args...)
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

// Stop stops the running daemon
func (d *Daemon) Stop() error {
	pid, err := d.ReadPID()
	if err != nil {
		return fmt.Errorf("daemon is not running or PID file not found: %w", err)
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("failed to find process: %w", err)
	}

	// Send SIGTERM
	if err := process.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("failed to stop daemon: %w", err)
	}

	// Remove PID file
	if err := d.RemovePID(); err != nil {
		return fmt.Errorf("failed to remove PID file: %w", err)
	}

	return nil
}

// GetLogPath returns the path to the log file
func (d *Daemon) GetLogPath() string {
	return d.logFile
}
