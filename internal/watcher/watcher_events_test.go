package watcher

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// recordingHandler records the latest event per path and serves
// TrackedPaths from the set of paths it has seen with contents.
type recordingHandler struct {
	mu      sync.Mutex
	latest  map[string][]byte // nil value = reported deleted
	seen    map[string]bool
	tracked map[string]bool
}

func newRecordingHandler() *recordingHandler {
	return &recordingHandler{
		latest:  make(map[string][]byte),
		seen:    make(map[string]bool),
		tracked: make(map[string]bool),
	}
}

func (h *recordingHandler) OnFileChange(path string, contents []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.latest[path] = contents
	h.seen[path] = true
	h.tracked[path] = contents != nil
}

func (h *recordingHandler) TrackedPaths(dir string) []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []string
	for p, live := range h.tracked {
		if live && strings.HasPrefix(p, dir+string(filepath.Separator)) {
			out = append(out, p)
		}
	}
	return out
}

func (h *recordingHandler) state(path string) (contents []byte, seen bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.latest[path], h.seen[path]
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func startWatcher(t *testing.T) (string, *recordingHandler) {
	t.Helper()
	root := t.TempDir()
	h := newRecordingHandler()
	w, err := New(h)
	if err != nil {
		t.Fatalf("new watcher: %v", err)
	}
	if err := w.Start(root); err != nil {
		t.Fatalf("start watcher: %v", err)
	}
	t.Cleanup(w.Stop)
	return root, h
}

func TestWatcherTracksFilesInNestedNewDirectories(t *testing.T) {
	root, h := startWatcher(t)

	// mkdir -p then write at once: only the top directory raises a Create
	// on a watched parent; nothing below it is watched yet.
	deep := filepath.Join(root, "a", "b", "c")
	if err := os.MkdirAll(deep, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(deep, "f.txt"), []byte("one\n"), 0644); err != nil {
		t.Fatal(err)
	}
	rel := filepath.Join("a", "b", "c", "f.txt")
	waitFor(t, "file in new nested dir", func() bool {
		c, _ := h.state(rel)
		return string(c) == "one\n"
	})

	// Later edits in the new directory must be seen too (it is now watched).
	if err := os.WriteFile(filepath.Join(deep, "f.txt"), []byte("two\n"), 0644); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "edit in new nested dir", func() bool {
		c, _ := h.state(rel)
		return string(c) == "two\n"
	})
}

func TestWatcherReportsFilesUnderMovedAwayDirectoryAsDeleted(t *testing.T) {
	root, h := startWatcher(t)

	dir := filepath.Join(root, "pkg", "inner")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "x.go"), []byte("package inner\n"), 0644); err != nil {
		t.Fatal(err)
	}
	rel := filepath.Join("pkg", "inner", "x.go")
	waitFor(t, "file to be tracked", func() bool {
		c, _ := h.state(rel)
		return c != nil
	})

	// Moving the directory out of the tree raises no per-file events.
	outside := filepath.Join(t.TempDir(), "moved")
	if err := os.Rename(filepath.Join(root, "pkg"), outside); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "deletion of file under moved dir", func() bool {
		c, seen := h.state(rel)
		return seen && c == nil
	})
}

func TestWatcherTreatsRenameAsDeleteOfOldName(t *testing.T) {
	root, h := startWatcher(t)

	oldPath := filepath.Join(root, "old.txt")
	if err := os.WriteFile(oldPath, []byte("content\n"), 0644); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "old.txt tracked", func() bool {
		c, _ := h.state("old.txt")
		return c != nil
	})

	if err := os.Rename(oldPath, filepath.Join(root, "new.txt")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "rename reported", func() bool {
		oldC, oldSeen := h.state("old.txt")
		newC, _ := h.state("new.txt")
		return oldSeen && oldC == nil && string(newC) == "content\n"
	})
}
