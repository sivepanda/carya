//go:build unix || linux || darwin

package daemon

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestAcquireLockAllowsOneHolder(t *testing.T) {
	dir := t.TempDir()
	d := New(filepath.Join(dir, "carya.pid"), filepath.Join(dir, "carya.log"))

	first, err := d.AcquireLock(0)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}

	start := time.Now()
	if _, err := d.AcquireLock(150 * time.Millisecond); !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("second acquire = %v, want ErrAlreadyRunning", err)
	}
	if waited := time.Since(start); waited < 150*time.Millisecond {
		t.Fatalf("second acquire gave up after %s, before its wait window", waited)
	}

	// Closing the holder (as process exit does) frees the lock.
	first.Close()
	third, err := d.AcquireLock(0)
	if err != nil {
		t.Fatalf("acquire after release: %v", err)
	}
	third.Close()
}

func TestAcquireLockWaitsOutAMomentaryHolder(t *testing.T) {
	dir := t.TempDir()
	d := New(filepath.Join(dir, "carya.pid"), filepath.Join(dir, "carya.log"))

	holder, err := d.AcquireLock(0)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(100 * time.Millisecond)
		holder.Close()
	}()

	f, err := d.AcquireLock(2 * time.Second)
	if err != nil {
		t.Fatalf("expected to get the lock once the momentary holder released it: %v", err)
	}
	f.Close()
}
