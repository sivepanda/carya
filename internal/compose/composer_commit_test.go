package compose

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"carya/internal/chunk"
	"carya/internal/store"
)

// baseRepo makes a repository with files committed as "base", and the test
// identity the composer's own `git commit` needs.
func baseRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	for k, v := range map[string]string{
		"GIT_AUTHOR_NAME": "T", "GIT_AUTHOR_EMAIL": "t@example.com",
		"GIT_COMMITTER_NAME": "T", "GIT_COMMITTER_EMAIL": "t@example.com",
	} {
		t.Setenv(k, v)
	}
	repo := t.TempDir()
	gitRun(t, repo, "init", "-q", "-b", "main")
	for name, content := range files {
		writeFile(t, repo, name, content)
	}
	gitRun(t, repo, "add", "-A")
	gitRun(t, repo, "commit", "-q", "-m", "base")
	return repo
}

func writeFile(t *testing.T, repo, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repo, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// chunkOf records path's working tree change against HEAD as a chunk, as
// the daemon would.
func chunkOf(t *testing.T, repo, path string) chunk.Chunk {
	t.Helper()
	diff := gitRun(t, repo, "diff", "HEAD", "--", ":(literal)"+path)
	if diff == "" {
		t.Fatalf("%s has no change to record", path)
	}
	now := time.Now()
	return chunk.Chunk{ID: chunk.ChunkID(path), FilePath: path, Diff: diff, StartTime: now, EndTime: now, Hash: chunk.ChunkHash(path)}
}

// composeAll commits every chunk with CreateCommit, as selecting them all
// in compose does, wants a plain commit, and returns the chunk store.
func composeAll(t *testing.T, repo, message string, chunks ...chunk.Chunk) *store.SQLiteStore {
	t.Helper()
	st := saveChunks(t, chunks...)
	c, err := New(st, repo)
	if err != nil {
		t.Fatal(err)
	}
	for i := range c.Chunks() {
		c.SetSelected(i, true)
	}
	res, err := c.CreateCommit(message)
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	if res.Warning != nil || res.ApplyConflict != nil {
		t.Fatalf("expected a plain commit, got warning=%+v conflict=%+v", res.Warning, res.ApplyConflict)
	}
	return st
}

// apply.whitespace=fix would strip the trailing blanks from what is
// committed, and =error would refuse the chunk.
func TestCreateCommitIgnoresApplyWhitespaceConfig(t *testing.T) {
	for _, setting := range []string{"fix", "error"} {
		t.Run(setting, func(t *testing.T) {
			repo := baseRepo(t, map[string]string{"f.txt": "x \n"})
			gitRun(t, repo, "config", "apply.whitespace", setting)
			work := "a  \nb\t\n"
			writeFile(t, repo, "f.txt", work)
			c := chunkOf(t, repo, "f.txt")

			st := composeAll(t, repo, "trailing blanks", c)
			if got := gitRun(t, repo, "show", "HEAD:f.txt"); got != work {
				t.Errorf("f.txt in HEAD = %q, want %q", got, work)
			}
			if s := gitRun(t, repo, "status", "--porcelain"); s != "" {
				t.Errorf("status:\n%s", s)
			}
			// Finding the committed chunk in HEAD must not trip over its
			// whitespace either.
			if got, _ := st.FindChunks("f.txt"); len(got) != 0 {
				t.Errorf("committed chunk should be pruned, still have %d", len(got))
			}
		})
	}
}
