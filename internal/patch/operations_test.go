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

	if err := CheckApply(patchText); err != nil {
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

	if err := CheckApply(stalePatch); err == nil {
		t.Fatal("expected stale patch check to fail")
	}
}
