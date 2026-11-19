package main

import (
	"os"
	"path/filepath"
	"testing"

	"carya/internal/engine"
)

// recordStackedChunks drives the daemon's chunk pipeline for file.txt, one
// flushed chunk per version. The shadow index keeps each chunk's result, so
// every chunk after the first is diffed against the previous chunk rather
// than against HEAD.
func recordStackedChunks(t *testing.T, versions []string) (root, dbPath string) {
	t.Helper()
	// Keep the engine away from the developer's real global config.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	root = t.TempDir()
	gitIn(t, root, "init", "-q")
	os.WriteFile(filepath.Join(root, ".gitignore"), []byte(".carya/\n"), 0644)
	os.WriteFile(filepath.Join(root, "file.txt"), []byte("one\ntwo\nthree\n"), 0644)
	gitIn(t, root, "add", "-A")
	gitIn(t, root, "-c", "user.name=T", "-c", "user.email=t@example.com", "commit", "-q", "-m", "base")

	caryaPath := filepath.Join(root, ".carya")
	if err := os.MkdirAll(caryaPath, 0755); err != nil {
		t.Fatal(err)
	}
	dbPath = filepath.Join(caryaPath, "chunks.db")

	e, err := engine.NewEngineWithShadow(dbPath, root, caryaPath)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer e.Stop()

	for _, v := range versions {
		os.WriteFile(filepath.Join(root, "file.txt"), []byte(v), 0644)
		e.OnFileChange("file.txt", []byte(v))
		if err := e.ForceFlush("file.txt"); err != nil {
			t.Fatalf("flush: %v", err)
		}
	}
	return root, dbPath
}

// Each version touches a line next to the previous change, so every later
// chunk's hunk context includes the earlier chunk's result.
var stackedVersions = []string{
	"one\nTWO\nthree\n",
	"one\nTWO\nTHREE\n",
	"ONE\nTWO\nTHREE\n",
}

func TestRunChunkSyncKeepsStackedChunks(t *testing.T) {
	root, dbPath := recordStackedChunks(t, stackedVersions)

	res, err := runChunkSync(root, dbPath)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if res.kept != 3 || res.pruned != 0 {
		t.Fatalf("sync kept %d, pruned %d; want kept 3, pruned 0", res.kept, res.pruned)
	}
}

func TestRunChunkSyncPrunesCommittedBottomOfStack(t *testing.T) {
	root, dbPath := recordStackedChunks(t, stackedVersions)

	// Commit only the first chunk's result, then put the working tree back.
	os.WriteFile(filepath.Join(root, "file.txt"), []byte(stackedVersions[0]), 0644)
	gitIn(t, root, "add", "file.txt")
	gitIn(t, root, "-c", "user.name=T", "-c", "user.email=t@example.com", "commit", "-q", "-m", "first")
	os.WriteFile(filepath.Join(root, "file.txt"), []byte(stackedVersions[2]), 0644)

	res, err := runChunkSync(root, dbPath)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if res.kept != 2 || res.pruned != 1 {
		t.Fatalf("sync kept %d, pruned %d; want kept 2, pruned 1", res.kept, res.pruned)
	}
}

func TestRunChunkSyncKeepsTextChunkRecordedOnBinaryChunk(t *testing.T) {
	// Keep the engine away from the developer's real global config.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	root := t.TempDir()
	gitIn(t, root, "init", "-q")
	os.WriteFile(filepath.Join(root, ".gitignore"), []byte(".carya/\n"), 0644)
	os.WriteFile(filepath.Join(root, "data.bin"), []byte("\x00\x01binary\n"), 0644)
	gitIn(t, root, "add", "-A")
	gitIn(t, root, "-c", "user.name=T", "-c", "user.email=t@example.com", "commit", "-q", "-m", "base")

	caryaPath := filepath.Join(root, ".carya")
	if err := os.MkdirAll(caryaPath, 0755); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(caryaPath, "chunks.db")
	e, err := engine.NewEngineWithShadow(dbPath, root, caryaPath)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer e.Stop()

	// The first chunk turns the binary file into text, so it is a binary
	// chunk; the second is a text chunk diffed against the text it left.
	for _, v := range []string{"one\ntwo\n", "one\nTWO\n"} {
		os.WriteFile(filepath.Join(root, "data.bin"), []byte(v), 0644)
		e.OnFileChange("data.bin", []byte(v))
		if err := e.ForceFlush("data.bin"); err != nil {
			t.Fatalf("flush: %v", err)
		}
	}

	res, err := runChunkSync(root, dbPath)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if res.kept != 2 || res.pruned != 0 {
		t.Fatalf("sync kept %d, pruned %d; want kept 2, pruned 0", res.kept, res.pruned)
	}
}
