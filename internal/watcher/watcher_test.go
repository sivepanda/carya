package watcher

import (
	"path/filepath"
	"testing"
)

func TestCaryaDirectoryIsAlwaysIgnored(t *testing.T) {
	root := t.TempDir()
	w := &Watcher{watchDir: root, gitignoreRules: []string{".git/"}}

	if !w.shouldIgnore(filepath.Join(root, ".carya"), true) {
		t.Fatal("expected .carya directory to be ignored")
	}
	if !w.shouldIgnore(filepath.Join(root, ".carya", "chunks.db"), false) {
		t.Fatal("expected files within .carya to be ignored")
	}
}
