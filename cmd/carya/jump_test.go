package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func exists(dir, rel string) bool {
	_, err := os.Stat(filepath.Join(dir, rel))
	return err == nil
}

// The teammate's tree adds b.txt and deletes a.txt relative to HEAD.
// Jumping must apply both; leaving must undo both.
func TestCheckoutTreeAppliesAdditionsAndDeletionsBothWays(t *testing.T) {
	repo := t.TempDir()
	gitIn(t, repo, "init", "-q")
	os.WriteFile(filepath.Join(repo, "a.txt"), []byte("a\n"), 0644)
	os.WriteFile(filepath.Join(repo, "keep.txt"), []byte("keep\n"), 0644)
	gitIn(t, repo, "add", "-A")
	gitIn(t, repo, "-c", "user.name=T", "-c", "user.email=t@example.com", "commit", "-q", "-m", "base")

	// Build the teammate's tree through a throwaway index.
	idx := filepath.Join(t.TempDir(), "index")
	env := append(os.Environ(), "GIT_INDEX_FILE="+idx)
	run := func(stdin string, args ...string) string {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		cmd.Env = env
		cmd.Stdin = strings.NewReader(stdin)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	run("", "read-tree", "HEAD")
	blob := run("b\n", "hash-object", "-w", "--stdin")
	run("", "update-index", "--add", "--cacheinfo", "100644,"+blob+",b.txt")
	run("", "update-index", "--force-remove", "a.txt")
	teammateTree := run("", "write-tree")

	os.WriteFile(filepath.Join(repo, "untracked.txt"), []byte("mine\n"), 0644)

	if err := checkoutTree(repo, teammateTree); err != nil {
		t.Fatalf("jump checkout: %v", err)
	}
	if !exists(repo, "b.txt") || exists(repo, "a.txt") || !exists(repo, "keep.txt") {
		t.Fatalf("after jump: b.txt=%v a.txt=%v keep.txt=%v; want true,false,true",
			exists(repo, "b.txt"), exists(repo, "a.txt"), exists(repo, "keep.txt"))
	}

	if err := checkoutTree(repo, "HEAD"); err != nil {
		t.Fatalf("leave checkout: %v", err)
	}
	if exists(repo, "b.txt") || !exists(repo, "a.txt") {
		t.Fatalf("after leave: b.txt=%v a.txt=%v; want false,true", exists(repo, "b.txt"), exists(repo, "a.txt"))
	}
	if status := gitIn(t, repo, "status", "--porcelain"); status != "?? untracked.txt" {
		t.Fatalf("after leave, status = %q; want only the untouched untracked file", status)
	}
}
