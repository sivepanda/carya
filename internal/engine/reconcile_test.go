package engine

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"carya/internal/chunk"
	"carya/internal/store"
)

func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
	return string(out)
}

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestReconcileWorkingTreeAtStartup(t *testing.T) {
	// Keep the engine away from the developer's real global config.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	root := t.TempDir()
	gitRun(t, root, "init", "-q")
	writeFile(t, root, ".gitignore", ".carya/\nbuild.log\n")
	for _, f := range []string{"mod.txt", "del.txt", "chunked.txt", "same.txt"} {
		writeFile(t, root, f, f+" base\n")
	}
	gitRun(t, root, "add", "-A")
	gitRun(t, root, "-c", "user.name=T", "-c", "user.email=t@example.com", "commit", "-q", "-m", "base")

	// Uncommitted work that exists before the daemon starts.
	writeFile(t, root, "mod.txt", "mod.txt changed\n")
	os.Remove(filepath.Join(root, "del.txt"))
	writeFile(t, root, "new.txt", "brand new\n")
	writeFile(t, root, "chunked.txt", "chunked.txt changed\n")
	writeFile(t, root, "build.log", "ignored by git\n")
	writeFile(t, root, "skip.tmp", "filtered by the watcher\n")

	caryaPath := filepath.Join(root, ".carya")
	if err := os.MkdirAll(caryaPath, 0755); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(caryaPath, "chunks.db")

	// chunked.txt already has a stored chunk from a previous daemon run.
	pre, err := store.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := pre.SaveChunk(chunk.Chunk{ID: "chunked-prev", FilePath: "chunked.txt", Diff: "prev", StartTime: time.Now(), EndTime: time.Now(), Hash: "h"}); err != nil {
		t.Fatal(err)
	}
	pre.Close()

	e, err := NewEngineWithShadow(dbPath, root, caryaPath)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	e.Start()
	defer e.Stop()

	shouldTrack := func(rel string) bool { return !strings.HasSuffix(rel, ".tmp") }
	if err := e.ReconcileWorkingTree(root, shouldTrack); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if err := e.FlushAll(); err != nil {
		t.Fatalf("flush: %v", err)
	}

	// Published tree: every tracked change from disk is present.
	tree, err := e.shadow.WriteTree()
	if err != nil {
		t.Fatal(err)
	}
	show := func(path string) (string, bool) {
		cmd := exec.Command("git", "show", tree+":"+path)
		cmd.Dir = root
		out, err := cmd.Output()
		return string(out), err == nil
	}
	for path, want := range map[string]string{
		"mod.txt":     "mod.txt changed\n",
		"new.txt":     "brand new\n",
		"chunked.txt": "chunked.txt changed\n",
		"same.txt":    "same.txt base\n",
	} {
		if got, ok := show(path); !ok || got != want {
			t.Errorf("published %s = %q (present=%v), want %q", path, got, ok, want)
		}
	}
	for _, path := range []string{"del.txt", "build.log", "skip.tmp"} {
		if _, ok := show(path); ok {
			t.Errorf("published tree should not contain %s", path)
		}
	}

	// Chunks: changes without prior chunks are tracked; chunked.txt is not
	// chunked a second time.
	st, err := store.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	for path, want := range map[string]int{"mod.txt": 1, "new.txt": 1, "del.txt": 1, "chunked.txt": 1, "same.txt": 0} {
		got, err := st.FindChunks(path)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != want {
			t.Errorf("chunks for %s = %d, want %d", path, len(got), want)
		}
	}
}
