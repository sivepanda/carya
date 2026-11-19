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

// stackRepo commits path at base, then returns one diff per version, each
// against the version before it, as the daemon records stacked chunks. The
// working tree is left at the last version with nothing staged.
func stackRepo(t *testing.T, path, base string, versions ...string) (string, []string) {
	t.Helper()
	for k, v := range map[string]string{
		"GIT_AUTHOR_NAME": "T", "GIT_AUTHOR_EMAIL": "t@example.com",
		"GIT_COMMITTER_NAME": "T", "GIT_COMMITTER_EMAIL": "t@example.com",
	} {
		t.Setenv(k, v)
	}
	repo := t.TempDir()
	gitRun(t, repo, "init", "-q")
	os.WriteFile(filepath.Join(repo, path), []byte(base), 0644)
	os.WriteFile(filepath.Join(repo, "other.txt"), []byte("other\n"), 0644)
	gitRun(t, repo, "add", "-A")
	gitRun(t, repo, "commit", "-q", "-m", "base")

	var diffs []string
	for _, v := range versions {
		os.WriteFile(filepath.Join(repo, path), []byte(v), 0644)
		diffs = append(diffs, gitRun(t, repo, "diff", "--", path))
		gitRun(t, repo, "add", path)
	}
	gitRun(t, repo, "reset", "-q")
	return repo, diffs
}

func saveChunks(t *testing.T, chunks ...chunk.Chunk) *store.SQLiteStore {
	t.Helper()
	st, err := store.NewSQLiteStore(filepath.Join(t.TempDir(), "chunks.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	for _, c := range chunks {
		if err := st.SaveChunk(c); err != nil {
			t.Fatal(err)
		}
	}
	return st
}

// The second chunk rewrites the first one's line, so it only applies on top
// of it, and the first no longer reverse-applies to the commit on its own.
var stackedVersions = []string{"one\nTWO\nthree\n", "one\nTWO!\nthree\n"}

func TestCommitStackedChunksTogether(t *testing.T) {
	repo, diffs := stackRepo(t, "file.txt", "one\ntwo\nthree\n", stackedVersions...)
	now := time.Now()
	st := saveChunks(t,
		chunk.Chunk{ID: "a", FilePath: "file.txt", Diff: diffs[0], StartTime: now, EndTime: now, Hash: "a"},
		chunk.Chunk{ID: "b", FilePath: "file.txt", Diff: diffs[1], StartTime: now.Add(time.Second), EndTime: now.Add(time.Second), Hash: "b"},
	)

	c, err := New(st, repo)
	if err != nil {
		t.Fatal(err)
	}
	for i := range c.Chunks() {
		c.SetSelected(i, true)
	}
	res, err := c.CreateCommit("stack")
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	if res.Warning != nil || res.ApplyConflict != nil {
		t.Fatalf("expected a plain commit, got warning=%v conflict=%v", res.Warning, res.ApplyConflict)
	}

	if got := gitRun(t, repo, "show", "HEAD:file.txt"); got != stackedVersions[1] {
		t.Fatalf("committed file.txt = %q, want %q", got, stackedVersions[1])
	}
	if got, _ := st.FindChunks("file.txt"); len(got) != 0 {
		t.Fatalf("both committed chunks should be pruned, still have %d", len(got))
	}
}

func TestApplyConflictKeepsStackApplicable(t *testing.T) {
	repo, diffs := stackRepo(t, "file.txt", "one\ntwo\nthree\n", stackedVersions...)
	stale := "diff --git a/other.txt b/other.txt\n--- a/other.txt\n+++ b/other.txt\n@@ -1 +1 @@\n-nope\n+still-nope\n"
	now := time.Now()
	st := saveChunks(t,
		chunk.Chunk{ID: "a", FilePath: "file.txt", Diff: diffs[0], StartTime: now, EndTime: now, Hash: "a"},
		chunk.Chunk{ID: "stale", FilePath: "other.txt", Diff: stale, StartTime: now.Add(500 * time.Millisecond), EndTime: now.Add(500 * time.Millisecond), Hash: "s"},
		chunk.Chunk{ID: "b", FilePath: "file.txt", Diff: diffs[1], StartTime: now.Add(time.Second), EndTime: now.Add(time.Second), Hash: "b"},
	)

	c, err := New(st, repo)
	if err != nil {
		t.Fatal(err)
	}
	for i := range c.Chunks() {
		c.SetSelected(i, true)
	}
	res, err := c.CreateCommit("stack")
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	conflict := res.ApplyConflict
	if conflict == nil {
		t.Fatal("expected an apply conflict for the stale chunk")
	}
	for i, ch := range c.Chunks() {
		if want := ch.FilePath == "file.txt"; conflict.Applicable[i] != want {
			t.Errorf("chunk %s applicable = %v, want %v", ch.ID, conflict.Applicable[i], want)
		}
	}
	if len(conflict.Skipped) != 1 || conflict.Skipped[0] != "other.txt (no longer matches)" {
		t.Fatalf("skipped = %q, want only the stale other.txt chunk", conflict.Skipped)
	}

	if _, err := c.ApplyApplicableCommit(conflict.Applicable, conflict.CommitMsg); err != nil {
		t.Fatalf("commit applicable: %v", err)
	}
	if got := gitRun(t, repo, "show", "HEAD:file.txt"); got != stackedVersions[1] {
		t.Fatalf("committed file.txt = %q, want %q", got, stackedVersions[1])
	}
	if got, _ := st.FindChunks("file.txt"); len(got) != 0 {
		t.Fatalf("committed stack should be pruned, still have %d", len(got))
	}
	if got, _ := st.FindChunks("other.txt"); len(got) != 1 {
		t.Fatalf("skipped chunk should be kept, have %d", len(got))
	}
}

func TestApplyConflictSkipsChunkOnUncommittedBinaryChange(t *testing.T) {
	// data.bin starts out binary; a binary chunk turns it into text and a
	// text chunk is recorded on top of that text.
	const asText = "one\ntwo\n"
	repo, diffs := stackRepo(t, "data.bin", "\x00\x01binary\n", asText, "one\nTWO\n")
	textFile := filepath.Join(t.TempDir(), "as-text")
	os.WriteFile(textFile, []byte(asText), 0644)
	blob := strings.TrimSpace(gitRun(t, repo, "hash-object", "-w", textFile))

	os.WriteFile(filepath.Join(repo, "other.txt"), []byte("other changed\n"), 0644)
	otherDiff := gitRun(t, repo, "diff", "--", "other.txt")

	now := time.Now()
	st := saveChunks(t,
		chunk.Chunk{ID: "bin", FilePath: "data.bin", Diff: "Binary file data.bin has changed\n(Initial hash: 00000000, Latest hash: " + blob[:8] + ")", StartTime: now, EndTime: now, Hash: chunk.ChunkHash(blob)},
		chunk.Chunk{ID: "text", FilePath: "data.bin", Diff: diffs[1], StartTime: now.Add(time.Second), EndTime: now.Add(time.Second), Hash: "t"},
		chunk.Chunk{ID: "other", FilePath: "other.txt", Diff: otherDiff, StartTime: now.Add(2 * time.Second), EndTime: now.Add(2 * time.Second), Hash: "o"},
	)

	c, err := New(st, repo)
	if err != nil {
		t.Fatal(err)
	}
	for i := range c.Chunks() {
		c.SetSelected(i, true)
	}
	res, err := c.CreateCommit("other")
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	conflict := res.ApplyConflict
	if conflict == nil {
		t.Fatal("expected an apply conflict for the chunk on the binary change")
	}
	// Compose cannot apply the binary change, so the text chunk on top of it
	// cannot be committed yet, but it is not stale either.
	if want := "data.bin (waits on an uncommitted binary change)"; len(conflict.Skipped) != 1 || conflict.Skipped[0] != want {
		t.Fatalf("skipped = %q, want only %q", conflict.Skipped, want)
	}

	if _, err := c.ApplyApplicableCommit(conflict.Applicable, conflict.CommitMsg); err != nil {
		t.Fatalf("commit applicable: %v", err)
	}
	if got := gitRun(t, repo, "show", "HEAD:other.txt"); got != "other changed\n" {
		t.Fatalf("committed other.txt = %q", got)
	}
	if got, _ := st.FindChunks("data.bin"); len(got) != 2 {
		t.Fatalf("data.bin's chunks should be kept, have %d", len(got))
	}
}
