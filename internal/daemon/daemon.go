package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ErrAlreadyRunning is returned when another daemon already serves the repository.
var ErrAlreadyRunning = errors.New("daemon is already running")

// AlreadyRunningExitCode is the exit status of a daemon process that found the
// daemon lock held by another daemon, so Start can tell that apart from a crash.
const AlreadyRunningExitCode = 3

const (
	// startupConfirmTimeout bounds how long Start waits for the new daemon
	// to record its PID before assuming it is still starting up.
	startupConfirmTimeout = 5 * time.Second
	// stopTimeout bounds how long Stop waits for the daemon to exit; on
	// SIGTERM it flushes chunks and may push to the remote first.
	stopTimeout = 30 * time.Second
)

// Daemon manages a background process with PID file
type Daemon struct {
	pidFile  string
	logFile  string
	lockFile string
}

type pidRecord struct {
	PID        int    `json:"pid"`
	Executable string `json:"executable"`
}

// New creates a new daemon manager
func New(pidFile, logFile string) *Daemon {
	return &Daemon{
		pidFile:  pidFile,
		logFile:  logFile,
		lockFile: filepath.Join(filepath.Dir(pidFile), "daemon.lock"),
	}
}

// IsRunning checks if the daemon is currently running
func (d *Daemon) IsRunning() bool {
	record, err := d.readPIDRecord()
	if err != nil {
		return false
	}
	return isProcessRunning(record.PID) && processMatches(record.PID, record.Executable)
}

// ReadPID reads the PID from the PID file
func (d *Daemon) ReadPID() (int, error) {
	record, err := d.readPIDRecord()
	if err != nil {
		return 0, err
	}
	return record.PID, nil
}

func (d *Daemon) readPIDRecord() (pidRecord, error) {
	data, err := os.ReadFile(d.pidFile)
	if err != nil {
		return pidRecord{}, err
	}
	var record pidRecord
	if err := json.Unmarshal(data, &record); err == nil && record.PID > 0 && record.Executable != "" {
		return record, nil
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		log.Printf("Invalid PID in file: %v", err)
		return pidRecord{}, fmt.Errorf("invalid PID in file: %w", err)
	}
	return pidRecord{PID: pid}, nil
}

// WritePID writes the current process PID to the PID file
func (d *Daemon) WritePID() error {
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve daemon executable: %w", err)
	}
	data, err := json.Marshal(pidRecord{PID: os.Getpid(), Executable: executable})
	if err != nil {
		return fmt.Errorf("encode pid record: %w", err)
	}
	return os.WriteFile(d.pidFile, append(data, '\n'), 0644)
}

// RemovePID removes the PID file
func (d *Daemon) RemovePID() error {
	return os.Remove(d.pidFile)
}

// RemoveOwnPID removes the PID file only if it still names the current
// process, so an exiting daemon can't delete the record of its successor.
func (d *Daemon) RemoveOwnPID() {
	d.removePIDIf(os.Getpid())
}

func (d *Daemon) removePIDIf(pid int) {
	if record, err := d.readPIDRecord(); err == nil && record.PID == pid {
		_ = d.RemovePID()
	}
}

// Start starts the daemon in background mode. It returns ErrAlreadyRunning
// when another daemon (possibly one started concurrently) holds the lock.
func (d *Daemon) Start(args []string) error {
	if d.IsRunning() {
		log.Printf("Daemon is already running")
		return ErrAlreadyRunning
	}

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(d.pidFile), 0755); err != nil {
		log.Printf("Failed to create daemon directory: %v", err)
		return fmt.Errorf("failed to create daemon directory: %w", err)
	}

	// Create log file
	logFile, err := os.OpenFile(d.logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		log.Printf("Failed to create log file: %v", err)
		return fmt.Errorf("failed to create log file: %w", err)
	}
	defer logFile.Close()

	// Get current executable
	executable, err := os.Executable()
	if err != nil {
		log.Printf("Failed to get executable path: %v", err)
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	// Start the process in background
	cmd := exec.Command(executable, args...)
	if err := startProcess(cmd, logFile); err != nil {
		return err
	}
	return d.confirmStarted(cmd)
}

// confirmStarted waits until the new daemon records its PID (it does so only
// after taking the daemon lock) or exits. A daemon that lost the lock to a
// concurrently started one exits with AlreadyRunningExitCode.
func (d *Daemon) confirmStarted(cmd *exec.Cmd) error {
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()

	deadline := time.NewTimer(startupConfirmTimeout)
	defer deadline.Stop()
	tick := time.NewTicker(25 * time.Millisecond)
	defer tick.Stop()

	for {
		select {
		case err := <-exited:
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) && exitErr.ExitCode() == AlreadyRunningExitCode {
				return ErrAlreadyRunning
			}
			return fmt.Errorf("daemon exited during startup; see %s", d.logFile)
		case <-tick.C:
			if record, err := d.readPIDRecord(); err == nil && record.PID == cmd.Process.Pid {
				return nil
			}
		case <-deadline.C:
			// Still alive but slow to start; it has not failed.
			return nil
		}
	}
}

// Stop signals the running daemon to shut down and waits for it to exit, so
// callers can rely on it no longer touching the repository.
func (d *Daemon) Stop() error {
	record, err := d.readPIDRecord()
	if err != nil {
		log.Printf("Daemon is not running or PID file not found: %v", err)
		return fmt.Errorf("daemon is not running or PID file not found: %w", err)
	}
	if record.Executable == "" || !isProcessRunning(record.PID) || !processMatches(record.PID, record.Executable) {
		return fmt.Errorf("pid file does not identify a running carya daemon")
	}

	if err := stopProcess(record.PID); err != nil {
		return err
	}

	deadline := time.Now().Add(stopTimeout)
	for isProcessRunning(record.PID) && processMatches(record.PID, record.Executable) {
		if time.Now().After(deadline) {
			return fmt.Errorf("daemon (PID %d) is still shutting down after %s", record.PID, stopTimeout)
		}
		time.Sleep(50 * time.Millisecond)
	}

	// The daemon removes its own PID file on a clean exit; this covers the
	// case where it could not.
	d.removePIDIf(record.PID)
	return nil
}

// GetLogPath returns the path to the log file
func (d *Daemon) GetLogPath() string {
	return d.logFile
}
