package compose

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

func TestCommitPrunesOnlyTheChunksItCommitted(t *testing.T) {
	// The composer's own `git commit` inherits the process environment, so
	// set an identity here rather than relying on the machine's git config.
	for k, v := range map[string]string{
		"GIT_AUTHOR_NAME": "T", "GIT_AUTHOR_EMAIL": "t@example.com",
		"GIT_COMMITTER_NAME": "T", "GIT_COMMITTER_EMAIL": "t@example.com",
	} {
		t.Setenv(k, v)
	}
	repo := t.TempDir()
	gitRun(t, repo, "init", "-q")
	os.WriteFile(filepath.Join(repo, "one.txt"), []byte("one\n"), 0644)
	os.WriteFile(filepath.Join(repo, "two.txt"), []byte("two\n"), 0644)
	gitRun(t, repo, "add", "-A")
	gitRun(t, repo, "commit", "-q", "-m", "base")

	os.WriteFile(filepath.Join(repo, "one.txt"), []byte("one changed\n"), 0644)
	os.WriteFile(filepath.Join(repo, "two.txt"), []byte("two changed\n"), 0644)
	diffOne := gitRun(t, repo, "diff", "--", "one.txt")
	diffTwo := gitRun(t, repo, "diff", "--", "two.txt")

	st, err := store.NewSQLiteStore(filepath.Join(t.TempDir(), "chunks.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	now := time.Now()
	for _, c := range []chunk.Chunk{
		{ID: "one", FilePath: "one.txt", Diff: diffOne, StartTime: now, EndTime: now, Hash: "1"},
		{ID: "two", FilePath: "two.txt", Diff: diffTwo, StartTime: now, EndTime: now.Add(time.Second), Hash: "2"},
	} {
		if err := st.SaveChunk(c); err != nil {
			t.Fatal(err)
		}
	}

	c, err := New(st, repo)
	if err != nil {
		t.Fatal(err)
	}
	// Select both, but commit only one.txt's patch (as when the other
	// chunk is skipped); the skipped chunk must survive.
	for i := range c.Chunks() {
		c.SetSelected(i, true)
	}
	if _, err := c.ApplyPatchAndCommit(diffOne, "commit one"); err != nil {
		t.Fatalf("commit: %v", err)
	}

	if got, _ := st.FindChunks("one.txt"); len(got) != 0 {
		t.Fatalf("committed chunk should be pruned, still have %d", len(got))
	}
	if got, _ := st.FindChunks("two.txt"); len(got) != 1 {
		t.Fatalf("uncommitted chunk should be kept, have %d", len(got))
	}
	if log := gitRun(t, repo, "log", "--oneline"); !strings.Contains(log, "commit one") {
		t.Fatalf("expected the commit to exist, log:\n%s", log)
	}
}
