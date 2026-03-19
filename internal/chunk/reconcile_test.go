package chunk

import (
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
