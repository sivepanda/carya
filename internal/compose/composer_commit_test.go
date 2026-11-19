package compose

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
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

// Committing while git is in the middle of a merge, cherry-pick or revert
// would make that operation's commit: a merge commit with only the chunk's
// changes, or one that takes the picked commit's author.
func TestCreateCommitRefusesDuringOperation(t *testing.T) {
	for _, op := range []struct {
		name  string
		start []string
	}{
		{"merge", []string{"merge", "-q", "--no-ff", "--no-commit", "other"}},
		{"cherry-pick", []string{"cherry-pick", "other"}},
		{"revert", []string{"revert", "--no-edit", "HEAD~1"}},
	} {
		t.Run(op.name, func(t *testing.T) {
			repo := baseRepo(t, map[string]string{"f.txt": "f\n", "c.txt": "base\n"})
			gitRun(t, repo, "checkout", "-q", "-b", "other")
			writeFile(t, repo, "c.txt", "other\n")
			gitRun(t, repo, "commit", "-q", "-am", "other")
			gitRun(t, repo, "checkout", "-q", "main")
			writeFile(t, repo, "c.txt", "main\n")
			gitRun(t, repo, "commit", "-q", "-am", "main")
			writeFile(t, repo, "c.txt", "main again\n")
			gitRun(t, repo, "commit", "-q", "-am", "main again")
			// The merge stops before committing; the cherry-pick and the
			// revert stop on their conflict in c.txt.
			cmd := exec.Command("git", op.start...)
			cmd.Dir = repo
			_ = cmd.Run()
			if _, err := os.Stat(filepath.Join(repo, ".git", strings.ToUpper(strings.ReplaceAll(op.name, "-", "_"))+"_HEAD")); err != nil {
				t.Fatalf("no %s in progress: %v", op.name, err)
			}
			writeFile(t, repo, "f.txt", "agent change\n")
			head := gitRun(t, repo, "rev-parse", "HEAD")

			c, err := New(saveChunks(t, chunkOf(t, repo, "f.txt")), repo)
			if err != nil {
				t.Fatal(err)
			}
			c.SetSelected(0, true)
			if _, err := c.CreateCommit("during " + op.name); err == nil || !strings.Contains(err.Error(), op.name) {
				t.Fatalf("CreateCommit during a %s: %v", op.name, err)
			}
			if got := gitRun(t, repo, "rev-parse", "HEAD"); got != head {
				t.Errorf("HEAD moved:\n%s", gitRun(t, repo, "log", "-1", "--format=%an %s%n%P", "HEAD"))
			}
		})
	}
}

// lines returns a file of n numbered lines, with the lines in change
// replaced.
func lines(n int, change map[int]string) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		if c, ok := change[i]; ok {
			b.WriteString(c + "\n")
			continue
		}
		fmt.Fprintf(&b, "line %d\n", i)
	}
	return b.String()
}

// A path the user has staged changes to keeps them in the index, with the
// committed change merged in, so their next `git commit` does not undo it.
func TestCreateCommitKeepsStagedChangeOfTheSameFile(t *testing.T) {
	t.Run("other change staged", func(t *testing.T) {
		repo := baseRepo(t, map[string]string{"f.txt": lines(30, nil)})
		writeFile(t, repo, "f.txt", lines(30, map[int]string{3: "staged 3"}))
		gitRun(t, repo, "add", "f.txt")
		writeFile(t, repo, "f.txt", lines(30, map[int]string{27: "agent 27"}))
		c := chunkOf(t, repo, "f.txt")
		writeFile(t, repo, "f.txt", lines(30, map[int]string{3: "staged 3", 27: "agent 27"}))

		composeAll(t, repo, "agent change", c)
		if got, want := gitRun(t, repo, "show", "HEAD:f.txt"), lines(30, map[int]string{27: "agent 27"}); got != want {
			t.Errorf("f.txt in HEAD:\n%s\nwant:\n%s", got, want)
		}
		want := lines(30, map[int]string{3: "staged 3", 27: "agent 27"})
		if got := gitRun(t, repo, "show", ":f.txt"); got != want {
			t.Errorf("f.txt in the index:\n%s\nwant:\n%s", got, want)
		}
		gitRun(t, repo, "commit", "-q", "-m", "user's commit")
		if got := gitRun(t, repo, "show", "HEAD:f.txt"); got != want {
			t.Errorf("f.txt after the user's commit:\n%s\nwant:\n%s", got, want)
		}
	})
	t.Run("whole file staged", func(t *testing.T) {
		repo := baseRepo(t, map[string]string{"f.txt": lines(30, nil)})
		writeFile(t, repo, "f.txt", lines(30, map[int]string{27: "agent 27"}))
		c := chunkOf(t, repo, "f.txt")
		work := lines(30, map[int]string{3: "staged 3", 27: "agent 27"})
		writeFile(t, repo, "f.txt", work)
		gitRun(t, repo, "add", "f.txt")

		composeAll(t, repo, "agent change", c)
		if got := gitRun(t, repo, "show", ":f.txt"); got != work {
			t.Errorf("f.txt in the index:\n%s\nwant:\n%s", got, work)
		}
		if s := gitRun(t, repo, "status", "--porcelain"); s != "M  f.txt\n" {
			t.Errorf("status:\n%s", s)
		}
	})
}

// A file named like a glob must not be treated as a pathspec that also
// matches the user's staged files.
func TestCreateCommitPathThatLooksLikeAGlob(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not allow * in file names")
	}
	repo := baseRepo(t, map[string]string{"a.txt": "a\n", "*.txt": "star\n"})
	writeFile(t, repo, "a.txt", "a staged\n")
	gitRun(t, repo, "add", "a.txt")
	writeFile(t, repo, "*.txt", "star changed\n")

	composeAll(t, repo, "star", chunkOf(t, repo, "*.txt"))
	if got := gitRun(t, repo, "show", "HEAD:*.txt"); got != "star changed\n" {
		t.Errorf("*.txt in HEAD = %q", got)
	}
	if got := gitRun(t, repo, "diff", "--cached", "--name-only"); got != "a.txt\n" {
		t.Errorf("staged after commit: %q, want only a.txt", got)
	}
	if got := gitRun(t, repo, "diff", "--name-only"); got != "" {
		t.Errorf("unstaged after commit: %q", got)
	}
}
