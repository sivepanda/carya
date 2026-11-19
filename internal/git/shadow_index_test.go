package git

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// newSeededShadow returns a shadow repo seeded from a main repo whose HEAD
// holds tracked.txt. Commands run without cmd.Dir, as in production, and the
// test's working directory is inside a different repository (this one).
func newSeededShadow(t *testing.T) (string, *ShadowRepo) {
	t.Helper()
	repoRoot := createMainRepo(t)
	shadow := NewShadowRepo(filepath.Join(repoRoot, ".carya"), repoRoot)
	if err := shadow.Initialize(); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if err := shadow.SeedIndexFromMainHEAD(); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return repoRoot, shadow
}

func addToShadowIndex(t *testing.T, shadow *ShadowRepo, path, content string) string {
	t.Helper()
	blob, err := shadow.HashObject([]byte(content))
	if err != nil {
		t.Fatalf("hash %s: %v", path, err)
	}
	if err := shadow.UpdateIndex(path, blob, "100644"); err != nil {
		t.Fatalf("update index %s: %v", path, err)
	}
	return blob
}

func TestRemoveFromIndexWorksInBareShadowRepo(t *testing.T) {
	repoRoot, shadow := newSeededShadow(t)

	if hash, _ := shadow.GetIndexEntry("tracked.txt"); hash == "" {
		t.Fatal("expected tracked.txt in the seeded index")
	}
	if err := shadow.RemoveFromIndex("tracked.txt"); err != nil {
		t.Fatalf("remove tracked.txt: %v", err)
	}
	if hash, _ := shadow.GetIndexEntry("tracked.txt"); hash != "" {
		t.Fatalf("expected tracked.txt removed, still at %s", hash)
	}

	tree, err := shadow.WriteTree()
	if err != nil {
		t.Fatalf("write tree: %v", err)
	}
	if out := runGit(t, repoRoot, "ls-tree", "--name-only", tree); out != "" {
		t.Fatalf("expected empty tree after removal, got %q", out)
	}

	if err := shadow.RemoveFromIndex("never-tracked.txt"); err != nil {
		t.Fatalf("removing an untracked path should be a no-op, got %v", err)
	}
}

func TestListIndexPathsMatchesDirectoryPrefixOnly(t *testing.T) {
	_, shadow := newSeededShadow(t)
	for _, p := range []string{"dir/a.txt", "dir/sub/b.txt", "dir2/x.txt", "dirfile"} {
		addToShadowIndex(t, shadow, p, p+"\n")
	}

	got, err := shadow.ListIndexPaths("dir")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	want := []string{"dir/a.txt", "dir/sub/b.txt"}
	if !slices.Equal(got, want) {
		t.Fatalf("ListIndexPaths(dir) = %v, want %v", got, want)
	}

	if got, _ := shadow.ListIndexPaths("missing"); len(got) != 0 {
		t.Fatalf("expected no entries under a missing dir, got %v", got)
	}
}

func TestGetIndexEntryIsLiteralAndExact(t *testing.T) {
	_, shadow := newSeededShadow(t)
	bracketBlob := addToShadowIndex(t, shadow, "page[id].tsx", "bracket\n")
	addToShadowIndex(t, shadow, "pagei.tsx", "glob-match\n")
	addToShadowIndex(t, shadow, "pkg/inner.go", "inner\n")

	if got, _ := shadow.GetIndexEntry("page[id].tsx"); got != bracketBlob {
		t.Fatalf("GetIndexEntry(page[id].tsx) = %q, want %q", got, bracketBlob)
	}
	if got, _ := shadow.GetIndexEntry("pkg"); got != "" {
		t.Fatalf("a directory name must not resolve to a file under it, got %q", got)
	}
}

func TestWorkingTreeChangesListsModifiedDeletedAndUntracked(t *testing.T) {
	repoRoot := createMainRepo(t)
	runGit(t, repoRoot, "rm", "-q", "--cached", "tracked.txt")
	runGit(t, repoRoot, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-q", "-m", "drop")
	for name, content := range map[string]string{"keep.txt": "k\n", "gone.txt": "g\n", "edit.txt": "e\n"} {
		if err := os.WriteFile(filepath.Join(repoRoot, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(repoRoot, ".gitignore"), []byte("ignored.txt\ntracked.txt\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repoRoot, "add", "keep.txt", "gone.txt", "edit.txt", ".gitignore")
	runGit(t, repoRoot, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-q", "-m", "files")

	os.Remove(filepath.Join(repoRoot, "gone.txt"))
	os.WriteFile(filepath.Join(repoRoot, "edit.txt"), []byte("changed\n"), 0644)
	os.WriteFile(filepath.Join(repoRoot, "new.txt"), []byte("n\n"), 0644)
	os.WriteFile(filepath.Join(repoRoot, "ignored.txt"), []byte("i\n"), 0644)

	got, err := WorkingTreeChanges(repoRoot)
	if err != nil {
		t.Fatalf("WorkingTreeChanges: %v", err)
	}
	slices.Sort(got)
	want := []string{"edit.txt", "gone.txt", "new.txt"}
	if !slices.Equal(got, want) {
		t.Fatalf("WorkingTreeChanges = %v, want %v", got, want)
	}
}
