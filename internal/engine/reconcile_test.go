package engine

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"carya/internal/chunk"
	"carya/internal/patch"
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

// applyToHEAD applies patchText to a temporary index seeded from HEAD and
// returns path's content there, as compose would commit it.
func applyToHEAD(t *testing.T, root, patchText, path string) string {
	t.Helper()
	env := append(os.Environ(), "GIT_INDEX_FILE="+filepath.Join(t.TempDir(), "index"))
	run := func(stdin string, args ...string) string {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		cmd.Env = env
		cmd.Stdin = strings.NewReader(stdin)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, out)
		}
		return string(out)
	}
	run("", "read-tree", "HEAD")
	run(patchText, "apply", "--cached", "-")
	return run("", "show", ":"+path)
}

func TestReconcileWorkingTreeChunksEditsMadeWhileStopped(t *testing.T) {
	// Keep the engine away from the developer's real global config.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	root := t.TempDir()
	gitRun(t, root, "init", "-q")
	writeFile(t, root, ".gitignore", ".carya/\n")
	writeFile(t, root, "file.txt", "one\ntwo\nthree\n")
	gitRun(t, root, "add", "-A")
	gitRun(t, root, "-c", "user.name=T", "-c", "user.email=t@example.com", "commit", "-q", "-m", "base")

	caryaPath := filepath.Join(root, ".carya")
	if err := os.MkdirAll(caryaPath, 0755); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(caryaPath, "chunks.db")

	// Each version touches a line next to the previous change, so every
	// later chunk's hunk context includes the earlier chunk's result.
	const (
		recorded = "one\nTWO\nthree\n" // saved while the daemon runs
		offline  = "one\nTWO\nTHREE\n" // saved while it is stopped
		later    = "ONE\nTWO\nTHREE\n" // saved after it restarts
	)

	first, err := NewEngineWithShadow(dbPath, root, caryaPath)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	writeFile(t, root, "file.txt", recorded)
	first.OnFileChange("file.txt", []byte(recorded))
	if err := first.ForceFlush("file.txt"); err != nil {
		t.Fatalf("flush: %v", err)
	}
	first.Stop()

	writeFile(t, root, "file.txt", offline)

	// The restarted daemon seeds its shadow index from HEAD again.
	e, err := NewEngineWithShadow(dbPath, root, caryaPath)
	if err != nil {
		t.Fatalf("restart engine: %v", err)
	}
	defer e.Stop()
	if err := e.ReconcileWorkingTree(root, nil); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if err := e.FlushAll(); err != nil {
		t.Fatalf("flush: %v", err)
	}

	writeFile(t, root, "file.txt", later)
	e.OnFileChange("file.txt", []byte(later))
	if err := e.ForceFlush("file.txt"); err != nil {
		t.Fatalf("flush: %v", err)
	}

	st, err := store.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	chunks, err := st.FindChunks("file.txt")
	if err != nil {
		t.Fatal(err)
	}

	// The offline edit is a chunk of its own, between the other two.
	if len(chunks) != 3 {
		t.Fatalf("chunks for file.txt = %d, want 3 (recorded, offline, later)", len(chunks))
	}
	for i, r := range chunk.DecideRetentionAll(root, chunks) {
		if r.Decision != chunk.DecisionKeep {
			t.Errorf("chunk %s: %s (%s), want keep\n%s", chunks[i].ID, r.Decision, r.Reason, chunks[i].Diff)
		}
	}

	// Composing the whole stack reproduces the file on disk.
	all := make(map[int]bool)
	for i := range chunks {
		all[i] = true
	}
	if got := applyToHEAD(t, root, patch.CreateFromChunks(chunks, all).Patch, "file.txt"); got != later {
		t.Fatalf("composed file.txt = %q, want %q", got, later)
	}
}
