package chunk

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(out))
	}
	return string(out)
}

func makeRepoWithFile(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	gitRun(t, repo, "init")
	if err := os.WriteFile(filepath.Join(repo, "file.txt"), []byte("line1\n"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	gitRun(t, repo, "add", "file.txt")
	gitRun(t, repo, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "initial")
	return repo
}

func TestDecideRetention(t *testing.T) {
	t.Run("empty diff prunes", func(t *testing.T) {
		repo := makeRepoWithFile(t)
		decision, reason := DecideRetention(repo, Chunk{FilePath: "file.txt", Diff: "  "})
		if decision != DecisionPrune || reason != "empty diff" {
			t.Fatalf("expected prune empty diff, got %s (%s)", decision, reason)
		}
	})

	t.Run("still applicable patch keeps", func(t *testing.T) {
		repo := makeRepoWithFile(t)
		if err := os.WriteFile(filepath.Join(repo, "file.txt"), []byte("line1\nline2\n"), 0644); err != nil {
			t.Fatalf("update file: %v", err)
		}
		patch := gitRun(t, repo, "diff", "--", "file.txt")
		if strings.TrimSpace(patch) == "" {
			t.Fatal("expected non-empty patch")
		}
		_ = gitRun(t, repo, "checkout", "--", "file.txt")

		decision, reason := DecideRetention(repo, Chunk{FilePath: "file.txt", Diff: patch})
		if decision != DecisionKeep || reason != "still applicable" {
			t.Fatalf("expected keep still applicable, got %s (%s)", decision, reason)
		}
	})

	t.Run("already applied patch prunes", func(t *testing.T) {
		repo := makeRepoWithFile(t)
		if err := os.WriteFile(filepath.Join(repo, "file.txt"), []byte("line1\nline2\n"), 0644); err != nil {
			t.Fatalf("update file: %v", err)
		}
		patch := gitRun(t, repo, "diff", "--", "file.txt")
		gitRun(t, repo, "add", "file.txt")
		gitRun(t, repo, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "apply patch")

		decision, reason := DecideRetention(repo, Chunk{FilePath: "file.txt", Diff: patch})
		if decision != DecisionPrune || reason != "already applied/committed" {
			t.Fatalf("expected prune already applied, got %s (%s)", decision, reason)
		}
	})

	t.Run("staged patch remains tracked", func(t *testing.T) {
		repo := makeRepoWithFile(t)
		if err := os.WriteFile(filepath.Join(repo, "file.txt"), []byte("line1\nline2\n"), 0644); err != nil {
			t.Fatalf("update file: %v", err)
		}
		patch := gitRun(t, repo, "diff", "--", "file.txt")
		gitRun(t, repo, "add", "file.txt")

		decision, reason := DecideRetention(repo, Chunk{FilePath: "file.txt", Diff: patch})
		if decision != DecisionKeep || reason != "still applicable" {
			t.Fatalf("expected staged patch to remain tracked, got %s (%s)", decision, reason)
		}
	})

	t.Run("already in upstream patch prunes", func(t *testing.T) {
		repo := makeRepoWithFile(t)

		remoteRoot := t.TempDir()
		remoteRepo := filepath.Join(remoteRoot, "remote.git")
		gitRun(t, repo, "init", "--bare", remoteRepo)
		gitRun(t, repo, "remote", "add", "origin", remoteRepo)
		gitRun(t, repo, "push", "-u", "origin", "HEAD")

		if err := os.WriteFile(filepath.Join(repo, "file.txt"), []byte("line1\nline2\n"), 0644); err != nil {
			t.Fatalf("update file: %v", err)
		}
		patch := gitRun(t, repo, "diff", "--", "file.txt")
		gitRun(t, repo, "add", "file.txt")
		gitRun(t, repo, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "apply patch")
		gitRun(t, repo, "push", "origin", "HEAD")

		gitRun(t, repo, "reset", "--hard", "HEAD~1")
		gitRun(t, repo, "fetch", "origin")

		decision, reason := DecideRetention(repo, Chunk{FilePath: "file.txt", Diff: patch})
		if decision != DecisionPrune || reason != "already in upstream" {
			t.Fatalf("expected prune already in upstream, got %s (%s)", decision, reason)
		}
	})

	t.Run("mismatched patch prunes", func(t *testing.T) {
		repo := makeRepoWithFile(t)
		patch := "diff --git a/file.txt b/file.txt\n--- a/file.txt\n+++ b/file.txt\n@@ -1 +1 @@\n-nope\n+still-nope\n"
		decision, reason := DecideRetention(repo, Chunk{FilePath: "file.txt", Diff: patch})
		if decision != DecisionPrune || reason != "no longer matches" {
			t.Fatalf("expected prune no longer matches, got %s (%s)", decision, reason)
		}
	})

	t.Run("binary path modified keeps", func(t *testing.T) {
		repo := makeRepoWithFile(t)
		if err := os.WriteFile(filepath.Join(repo, "bin.dat"), []byte{0, 1, 2, 3}, 0644); err != nil {
			t.Fatalf("write binary: %v", err)
		}
		gitRun(t, repo, "add", "bin.dat")
		gitRun(t, repo, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "add binary")

		if err := os.WriteFile(filepath.Join(repo, "bin.dat"), []byte{0, 1, 2, 3, 4}, 0644); err != nil {
			t.Fatalf("modify binary: %v", err)
		}
		decision, reason := DecideRetention(repo, Chunk{FilePath: "bin.dat", Diff: "Binary file bin.dat has changed", EndTime: time.Now()})
		if decision != DecisionKeep || reason != "binary path still modified" {
			t.Fatalf("expected keep for modified binary, got %s (%s)", decision, reason)
		}
	})
}

// makeRepoWith commits files as the repository's first commit.
func makeRepoWith(t *testing.T, files map[string]string) string {
	t.Helper()
	repo := t.TempDir()
	gitRun(t, repo, "init", "-q")
	for path, content := range files {
		writeRepoFile(t, repo, path, content)
	}
	gitRun(t, repo, "add", "-A")
	commitAll(t, repo, "base")
	return repo
}

func writeRepoFile(t *testing.T, repo, path, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repo, path), []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func commitAll(t *testing.T, repo, msg string) {
	t.Helper()
	gitRun(t, repo, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-q", "-a", "-m", msg)
}

// recordStack records one chunk per version of path the way the daemon does:
// the first is diffed against HEAD and each later one against the version
// before it. Chunk i starts at start+i seconds. The repository is reset to
// HEAD afterwards.
func recordStack(t *testing.T, repo, path string, start time.Time, versions ...string) []Chunk {
	t.Helper()
	var chunks []Chunk
	for i, v := range versions {
		writeRepoFile(t, repo, path, v)
		diff := gitRun(t, repo, "diff", "--", path) // against the previous version, staged below
		if strings.TrimSpace(diff) == "" {
			t.Fatalf("version %d of %s made no diff", i, path)
		}
		gitRun(t, repo, "add", path)
		at := start.Add(time.Duration(i) * time.Second)
		chunks = append(chunks, Chunk{
			ID:        ChunkID(fmt.Sprintf("%s-%d", path, at.UnixNano())),
			FilePath:  path,
			Diff:      diff,
			StartTime: at,
			EndTime:   at.Add(time.Millisecond),
		})
	}
	gitRun(t, repo, "reset", "-q", "--hard")
	return chunks
}

// newestFirst returns chunks in the order the store hands them out.
func newestFirst(chunks ...Chunk) []Chunk {
	out := make([]Chunk, len(chunks))
	for i, c := range chunks {
		out[len(chunks)-1-i] = c
	}
	return out
}

func checkRetention(t *testing.T, chunks []Chunk, got []Retention, want map[ChunkID]Retention) {
	t.Helper()
	if len(got) != len(chunks) {
		t.Fatalf("got %d decisions for %d chunks", len(got), len(chunks))
	}
	for i, c := range chunks {
		if got[i] != want[c.ID] {
			t.Errorf("%s: got %s (%s), want %s (%s)", c.ID, got[i].Decision, got[i].Reason, want[c.ID].Decision, want[c.ID].Reason)
		}
	}
}

var (
	keep      = Retention{DecisionKeep, "still applicable"}
	committed = Retention{DecisionPrune, "already applied/committed"}
	upstream  = Retention{DecisionPrune, "already in upstream"}
	noMatch   = Retention{DecisionPrune, "no longer matches"}
)

const stackBase = "one\ntwo\nthree\n"

// Each version changes a line next to the previous change, so every later
// chunk's hunk context includes the earlier chunk's result.
var stackVersions = []string{
	"one\nTWO\nthree\n",
	"one\nTWO\nTHREE\n",
	"ONE\nTWO\nTHREE\n",
}

func TestDecideRetentionAllStackedChunks(t *testing.T) {
	start := time.Now()

	t.Run("whole stack kept", func(t *testing.T) {
		repo := makeRepoWith(t, map[string]string{"file.txt": stackBase})
		s := recordStack(t, repo, "file.txt", start, stackVersions...)

		// Judged on its own against HEAD, a later chunk does not apply.
		if d, reason := DecideRetention(repo, s[1]); d != DecisionPrune || reason != "no longer matches" {
			t.Fatalf("precondition: chunk 2 alone should not apply to HEAD, got %s (%s)", d, reason)
		}

		chunks := newestFirst(s...)
		checkRetention(t, chunks, DecideRetentionAll(repo, chunks), map[ChunkID]Retention{
			s[0].ID: keep, s[1].ID: keep, s[2].ID: keep,
		})
	})

	t.Run("committed bottom chunk pruned, chunks above kept", func(t *testing.T) {
		repo := makeRepoWith(t, map[string]string{"file.txt": stackBase})
		s := recordStack(t, repo, "file.txt", start, stackVersions...)
		writeRepoFile(t, repo, "file.txt", stackVersions[0])
		commitAll(t, repo, "first chunk")

		chunks := newestFirst(s...)
		checkRetention(t, chunks, DecideRetentionAll(repo, chunks), map[ChunkID]Retention{
			s[0].ID: committed, s[1].ID: keep, s[2].ID: keep,
		})
	})

	t.Run("stack committed together counts as committed", func(t *testing.T) {
		// The second chunk rewrites the first one's line, so the first no
		// longer reverse-applies to HEAD on its own.
		repo := makeRepoWith(t, map[string]string{"file.txt": stackBase})
		s := recordStack(t, repo, "file.txt", start, "one\nTWO\nthree\n", "one\nTWO!\nthree\n")
		writeRepoFile(t, repo, "file.txt", "one\nTWO!\nthree\n")
		commitAll(t, repo, "both chunks")

		if AppliedAtHEAD(repo, s[0]) {
			t.Fatal("precondition: chunk 1 alone should not reverse-apply to HEAD")
		}

		chunks := newestFirst(s...)
		checkRetention(t, chunks, DecideRetentionAll(repo, chunks), map[ChunkID]Retention{
			s[0].ID: committed, s[1].ID: committed,
		})
		if got := AppliedAtHEADAll(repo, chunks); !got[0] || !got[1] {
			t.Fatalf("AppliedAtHEADAll = %v, want both applied", got)
		}
	})

	t.Run("stack in upstream only is pruned", func(t *testing.T) {
		repo := makeRepoWith(t, map[string]string{"file.txt": stackBase})
		remote := filepath.Join(t.TempDir(), "remote.git")
		gitRun(t, repo, "init", "-q", "--bare", remote)
		gitRun(t, repo, "remote", "add", "origin", remote)
		gitRun(t, repo, "push", "-q", "-u", "origin", "HEAD")

		s := recordStack(t, repo, "file.txt", start, "one\nTWO\nthree\n", "one\nTWO!\nthree\n")
		writeRepoFile(t, repo, "file.txt", "one\nTWO!\nthree\n")
		commitAll(t, repo, "both chunks")
		gitRun(t, repo, "push", "-q", "origin", "HEAD")
		gitRun(t, repo, "reset", "-q", "--hard", "HEAD~1")

		chunks := newestFirst(s...)
		checkRetention(t, chunks, DecideRetentionAll(repo, chunks), map[ChunkID]Retention{
			s[0].ID: upstream, s[1].ID: upstream,
		})
	})

	t.Run("stack that no longer matches is pruned", func(t *testing.T) {
		repo := makeRepoWith(t, map[string]string{"file.txt": stackBase})
		s := recordStack(t, repo, "file.txt", start, stackVersions[:2]...)
		writeRepoFile(t, repo, "file.txt", "uno\ndos\ntres\n")
		commitAll(t, repo, "rewrite")

		chunks := newestFirst(s...)
		checkRetention(t, chunks, DecideRetentionAll(repo, chunks), map[ChunkID]Retention{
			s[0].ID: noMatch, s[1].ID: noMatch,
		})
	})

	t.Run("mismatched chunk in a stack is pruned, the rest kept", func(t *testing.T) {
		repo := makeRepoWith(t, map[string]string{"file.txt": stackBase})
		s := recordStack(t, repo, "file.txt", start, stackVersions[:2]...)
		bad := Chunk{
			ID:        "file.txt-bad",
			FilePath:  "file.txt",
			Diff:      "diff --git a/file.txt b/file.txt\n--- a/file.txt\n+++ b/file.txt\n@@ -1 +1 @@\n-nope\n+still-nope\n",
			StartTime: start.Add(500 * time.Millisecond), // between the two real chunks
		}

		chunks := []Chunk{s[1], bad, s[0]}
		checkRetention(t, chunks, DecideRetentionAll(repo, chunks), map[ChunkID]Retention{
			s[0].ID: keep, bad.ID: noMatch, s[1].ID: keep,
		})
	})

	t.Run("stacks on different files are independent", func(t *testing.T) {
		repo := makeRepoWith(t, map[string]string{"file.txt": stackBase, "other.txt": stackBase})
		f := recordStack(t, repo, "file.txt", start, stackVersions[:2]...)
		// other.txt's chunks interleave in time with file.txt's.
		o := recordStack(t, repo, "other.txt", start.Add(500*time.Millisecond), stackVersions...)
		writeRepoFile(t, repo, "other.txt", stackVersions[0])
		commitAll(t, repo, "other.txt first chunk")

		chunks := []Chunk{o[2], f[1], o[1], f[0], o[0]}
		checkRetention(t, chunks, DecideRetentionAll(repo, chunks), map[ChunkID]Retention{
			f[0].ID: keep, f[1].ID: keep,
			o[0].ID: committed, o[1].ID: keep, o[2].ID: keep,
		})
	})
}
