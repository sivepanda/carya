package daemon

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// Daemon manages a background process with PID file
type Daemon struct {
	pidFile string
	logFile string
}

type pidRecord struct {
	PID        int    `json:"pid"`
	Executable string `json:"executable"`
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

// Start starts the daemon in background mode
func (d *Daemon) Start(args []string) error {
	if d.IsRunning() {
		log.Printf("Daemon is already running")
		return fmt.Errorf("daemon is already running")
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

	return startProcess(cmd, logFile)
}

// Stop stops the running daemon
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

	// Remove PID file
	if err := d.RemovePID(); err != nil {
		log.Printf("Failed to remove PID file: %v", err)
		return fmt.Errorf("failed to remove PID file: %w", err)
	}

	return nil
}

// GetLogPath returns the path to the log file
func (d *Daemon) GetLogPath() string {
	return d.logFile
}
