package patch

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
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

func withRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	gitRun(t, repo, "init")
	gitRun(t, repo, "config", "user.name", "Test")
	gitRun(t, repo, "config", "user.email", "test@example.com")
	if err := os.WriteFile(filepath.Join(repo, "file.txt"), []byte("line1\n"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	gitRun(t, repo, "add", "file.txt")
	gitRun(t, repo, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "initial")
	return repo
}

func TestCheckApplyUsesCachedIndex(t *testing.T) {
	repo := withRepo(t)

	if err := os.WriteFile(filepath.Join(repo, "file.txt"), []byte("line1\nline2\n"), 0644); err != nil {
		t.Fatalf("update file: %v", err)
	}

	patchText := gitRun(t, repo, "diff", "--", "file.txt")
	if strings.TrimSpace(patchText) == "" {
		t.Fatal("expected patch content")
	}

	oldWd, _ := os.Getwd()
	defer func() { _ = os.Chdir(oldWd) }()
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("chdir repo: %v", err)
	}

	if err := CheckApply(repo, patchText); err != nil {
		t.Fatalf("expected cached check to pass, got %v", err)
	}
}

func TestCheckApplyFailsWhenPatchIsStale(t *testing.T) {
	repo := withRepo(t)
	stalePatch := "diff --git a/file.txt b/file.txt\n--- a/file.txt\n+++ b/file.txt\n@@ -1 +1 @@\n-nope\n+still-nope\n"

	oldWd, _ := os.Getwd()
	defer func() { _ = os.Chdir(oldWd) }()
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("chdir repo: %v", err)
	}

	if err := CheckApply(repo, stalePatch); err == nil {
		t.Fatal("expected stale patch check to fail")
	}
}

func TestApplyAndCommitLeavesIndexClean(t *testing.T) {
	repo := withRepo(t)

	if err := os.WriteFile(filepath.Join(repo, "file.txt"), []byte("line1\nline2\n"), 0644); err != nil {
		t.Fatalf("update file: %v", err)
	}
	patchText := gitRun(t, repo, "diff", "--", "file.txt")

	if _, err := ApplyAndCommit(repo, patchText, "add line2"); err != nil {
		t.Fatalf("apply and commit: %v", err)
	}

	if staged := strings.TrimSpace(gitRun(t, repo, "diff", "--cached")); staged != "" {
		t.Fatalf("expected clean index after commit, got staged diff:\n%s", staged)
	}
	if status := strings.TrimSpace(gitRun(t, repo, "status", "--porcelain")); status != "" {
		t.Fatalf("expected clean status after commit, got:\n%s", status)
	}
}

func TestApplyAndCommitPreservesUnrelatedStagedChanges(t *testing.T) {
	repo := withRepo(t)

	if err := os.WriteFile(filepath.Join(repo, "other.txt"), []byte("staged\n"), 0644); err != nil {
		t.Fatalf("write other file: %v", err)
	}
	gitRun(t, repo, "add", "other.txt")

	if err := os.WriteFile(filepath.Join(repo, "file.txt"), []byte("line1\nline2\n"), 0644); err != nil {
		t.Fatalf("update file: %v", err)
	}
	patchText := gitRun(t, repo, "diff", "--", "file.txt")

	if _, err := ApplyAndCommit(repo, patchText, "add line2"); err != nil {
		t.Fatalf("apply and commit: %v", err)
	}

	status := strings.TrimSpace(gitRun(t, repo, "status", "--porcelain"))
	if status != "A  other.txt" {
		t.Fatalf("expected only other.txt to remain staged, got:\n%s", status)
	}
}

func TestApplyAndCommitOnUnbornBranch(t *testing.T) {
	repo := t.TempDir()
	gitRun(t, repo, "init")

	if err := os.WriteFile(filepath.Join(repo, "new.txt"), []byte("hello\n"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	newFilePatch := "diff --git a/new.txt b/new.txt\nnew file mode 100644\n--- /dev/null\n+++ b/new.txt\n@@ -0,0 +1 @@\n+hello\n"

	if err := CheckApply(repo, newFilePatch); err != nil {
		t.Fatalf("check apply on unborn branch: %v", err)
	}

	gitRun(t, repo, "config", "user.name", "Test")
	gitRun(t, repo, "config", "user.email", "test@example.com")
	if _, err := ApplyAndCommit(repo, newFilePatch, "first commit"); err != nil {
		t.Fatalf("apply and commit on unborn branch: %v", err)
	}

	if status := strings.TrimSpace(gitRun(t, repo, "status", "--porcelain")); status != "" {
		t.Fatalf("expected clean status after first commit, got:\n%s", status)
	}
}
