//go:build unix || linux || darwin

package daemon

import (
	"os"
	"testing"
)

func TestProcessMatchesOwnExecutable(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("resolve test executable: %v", err)
	}

	if !processMatches(os.Getpid(), executable) {
		t.Error("expected current process to match its own executable")
	}

	if _, err := os.Stat("/proc"); err == nil {
		if processMatches(os.Getpid(), "/nonexistent/other-binary") {
			t.Error("expected mismatched executable path to fail on a /proc system")
		}
	}
}
