package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func lsTree(t *testing.T, repo, tree string) map[string]string {
	t.Helper()
	entries := make(map[string]string)
	for _, line := range strings.Split(strings.TrimSpace(runGit(t, repo, "ls-tree", "-r", tree)), "\n") {
		if line == "" {
			continue
		}
		meta, path, _ := strings.Cut(line, "\t")
		entries[path] = meta
	}
	return entries
}

func TestWorkingStateTreeCapturesWorkingTreeWithoutTouchingIndex(t *testing.T) {
	repo := createMainRepo(t)
	commitFiles(t, repo, map[string]string{"gone.txt": "bye\n", "staged.txt": "old\n"}, "more")
	write := func(path, content string) {
		t.Helper()
		full := filepath.Join(repo, path)
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("tracked.txt", "edited by a shell command\n")
	write("staged.txt", "staged\n")
	runGit(t, repo, "add", "staged.txt")
	write("staged.txt", "staged then edited again\n")
	write("new/deep/file.txt", "untracked\n")
	write(".gitignore", ".carya/\nbuild/\n")
	write("build/out.bin", "ignored\n")
	write(".carya/user-id", "me\n")
	if err := os.Remove(filepath.Join(repo, "gone.txt")); err != nil {
		t.Fatal(err)
	}
	// A linked worktree inside the checkout, as `claude --worktree` makes.
	runGit(t, repo, "worktree", "add", "-q", filepath.Join(repo, ".claude", "worktrees", "wt"))

	statusBefore := runGit(t, repo, "status", "--porcelain")
	indexBefore, err := os.ReadFile(filepath.Join(repo, ".git", "index"))
	if err != nil {
		t.Fatal(err)
	}

	tree, err := WorkingStateTree(repo)
	if err != nil {
		t.Fatal(err)
	}
	got := lsTree(t, repo, tree)

	for _, want := range []string{"tracked.txt", "staged.txt", "new/deep/file.txt", ".gitignore"} {
		if _, ok := got[want]; !ok {
			t.Errorf("state is missing %s: %v", want, got)
		}
	}
	for _, unwanted := range []string{"gone.txt", "build/out.bin", ".carya/user-id", ".claude/worktrees/wt"} {
		if _, ok := got[unwanted]; ok {
			t.Errorf("state should not contain %s", unwanted)
		}
	}
	if blob := strings.TrimSpace(runGit(t, repo, "rev-parse", tree+":staged.txt")); blob != strings.TrimSpace(runGit(t, repo, "hash-object", "staged.txt")) {
		t.Error("staged.txt should hold the working-tree content, not the staged one")
	}

	indexAfter, err := os.ReadFile(filepath.Join(repo, ".git", "index"))
	if err != nil {
		t.Fatal(err)
	}
	if string(indexBefore) != string(indexAfter) || statusBefore != runGit(t, repo, "status", "--porcelain") {
		t.Fatal("snapshot changed the repository's index or status")
	}
}

func TestWorkingStateTreeInFreshRepo(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init", "-q")
	if err := os.WriteFile(filepath.Join(repo, "first.txt"), []byte("hi\n"), 0644); err != nil {
		t.Fatal(err)
	}
	tree, err := WorkingStateTree(repo)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := lsTree(t, repo, tree)["first.txt"]; !ok {
		t.Fatal("untracked file in a repo without commits is missing from the state")
	}
}
