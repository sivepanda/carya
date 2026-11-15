package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(out))
	}
	return string(out)
}

func createMainRepo(t *testing.T) string {
	t.Helper()
	repoRoot := t.TempDir()
	runGit(t, repoRoot, "init")

	filePath := filepath.Join(repoRoot, "tracked.txt")
	if err := os.WriteFile(filePath, []byte("base\n"), 0644); err != nil {
		t.Fatalf("write tracked file: %v", err)
	}
	runGit(t, repoRoot, "add", "tracked.txt")
	runGit(t, repoRoot, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "initial")

	return repoRoot
}

func TestShadowRepoInitializeRemovesStaleAlternates(t *testing.T) {
	repoRoot := createMainRepo(t)
	caryaPath := filepath.Join(repoRoot, ".carya")
	repo := NewShadowRepo(caryaPath, repoRoot)
	if err := repo.Initialize(); err != nil {
		t.Fatalf("first initialize: %v", err)
	}

	// Simulate the bidirectional alternates an older version left behind.
	shadowObjects := filepath.Join(repo.GitDir(), "objects")
	mainObjects := filepath.Join(repoRoot, ".git", "objects")
	mainAlt := filepath.Join(mainObjects, "info", "alternates")
	shadowAlt := filepath.Join(shadowObjects, "info", "alternates")
	for altFile, target := range map[string]string{
		mainAlt:   shadowObjects,
		shadowAlt: mainObjects,
	} {
		if err := os.MkdirAll(filepath.Dir(altFile), 0755); err != nil {
			t.Fatalf("mkdir for %s: %v", altFile, err)
		}
		if err := os.WriteFile(altFile, []byte(target+"\n"), 0644); err != nil {
			t.Fatalf("write %s: %v", altFile, err)
		}
	}

	if err := repo.Initialize(); err != nil {
		t.Fatalf("reinitialize shadow repo: %v", err)
	}

	if _, err := os.Stat(filepath.Join(repo.GitDir(), "HEAD")); err != nil {
		t.Fatalf("expected bare repo HEAD file: %v", err)
	}
	if _, err := os.Stat(mainAlt); !os.IsNotExist(err) {
		t.Fatalf("expected main alternates to be removed, stat err: %v", err)
	}
	if _, err := os.Stat(shadowAlt); !os.IsNotExist(err) {
		t.Fatalf("expected shadow alternates to be removed, stat err: %v", err)
	}
}

func TestShadowRepoInitializePreservesForeignAlternates(t *testing.T) {
	repoRoot := createMainRepo(t)
	repo := NewShadowRepo(filepath.Join(repoRoot, ".carya"), repoRoot)

	foreign := "/some/other/objects"
	mainAlt := filepath.Join(repoRoot, ".git", "objects", "info", "alternates")
	if err := os.MkdirAll(filepath.Dir(mainAlt), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	shadowObjects := filepath.Join(repo.GitDir(), "objects")
	content := foreign + "\n" + shadowObjects + "\n"
	if err := os.WriteFile(mainAlt, []byte(content), 0644); err != nil {
		t.Fatalf("write main alternates: %v", err)
	}

	if err := repo.Initialize(); err != nil {
		t.Fatalf("initialize shadow repo: %v", err)
	}

	data, err := os.ReadFile(mainAlt)
	if err != nil {
		t.Fatalf("read main alternates: %v", err)
	}
	if got := strings.TrimSpace(string(data)); got != foreign {
		t.Fatalf("expected only foreign alternate %q to remain, got %q", foreign, got)
	}
}

func TestShadowRepoWritesObjectsToMainStore(t *testing.T) {
	repoRoot := createMainRepo(t)
	repo := NewShadowRepo(filepath.Join(repoRoot, ".carya"), repoRoot)
	if err := repo.Initialize(); err != nil {
		t.Fatalf("initialize shadow repo: %v", err)
	}

	blobHash, err := repo.HashObject([]byte("shared store\n"))
	if err != nil {
		t.Fatalf("hash object: %v", err)
	}
	if err := repo.UpdateIndex("file.txt", blobHash, "100644"); err != nil {
		t.Fatalf("update index: %v", err)
	}
	treeHash, err := repo.WriteTree()
	if err != nil {
		t.Fatalf("write tree: %v", err)
	}

	// Both objects must be readable by the main repo without any alternates,
	// otherwise refs/carya/* refs cannot point at them and gc cannot protect them.
	for _, hash := range []string{blobHash, treeHash} {
		cmd := exec.Command("git", "cat-file", "-e", hash)
		cmd.Dir = repoRoot
		if err := cmd.Run(); err != nil {
			t.Fatalf("object %s not visible from main repo: %v", hash, err)
		}
	}
	runGit(t, repoRoot, "update-ref", "refs/carya/users/test/tree", treeHash)
}

func TestShadowRepoObjectAndIndexFlow(t *testing.T) {
	repoRoot := createMainRepo(t)
	repo := NewShadowRepo(filepath.Join(repoRoot, ".carya"), repoRoot)
	if err := repo.Initialize(); err != nil {
		t.Fatalf("initialize shadow repo: %v", err)
	}

	blobContent := []byte("hello from shadow\n")
	blobHash, err := repo.HashObject(blobContent)
	if err != nil {
		t.Fatalf("hash object: %v", err)
	}
	if blobHash == "" {
		t.Fatal("expected non-empty blob hash")
	}
	if !repo.ObjectExists(blobHash) {
		t.Fatal("expected blob to exist")
	}

	roundTrip, err := repo.GetObjectContent(blobHash)
	if err != nil {
		t.Fatalf("get object content: %v", err)
	}
	if string(roundTrip) != string(blobContent) {
		t.Fatalf("unexpected blob content: %q", string(roundTrip))
	}

	if err := repo.UpdateIndex("nested/file.txt", blobHash, "100644"); err != nil {
		t.Fatalf("update index: %v", err)
	}

	indexHash, err := repo.GetIndexEntry("nested/file.txt")
	if err != nil {
		t.Fatalf("get index entry: %v", err)
	}
	if indexHash != blobHash {
		t.Fatalf("expected index hash %q, got %q", blobHash, indexHash)
	}

	treeHash, err := repo.WriteTree()
	if err != nil {
		t.Fatalf("write tree: %v", err)
	}
	if treeHash == "" {
		t.Fatal("expected non-empty tree hash")
	}

	entries, err := repo.ListTree(treeHash)
	if err != nil {
		t.Fatalf("list tree: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 tree entry, got %d", len(entries))
	}
	if entries[0].Path != "nested/file.txt" {
		t.Fatalf("unexpected tree path: %q", entries[0].Path)
	}

	updatedBlobHash, err := repo.HashObject([]byte("updated\n"))
	if err != nil {
		t.Fatalf("hash updated object: %v", err)
	}
	if err := repo.UpdateIndex("nested/file.txt", updatedBlobHash, "100644"); err != nil {
		t.Fatalf("update index with second blob: %v", err)
	}
	mutatedHash, err := repo.GetIndexEntry("nested/file.txt")
	if err != nil {
		t.Fatalf("get index entry after mutation: %v", err)
	}
	if mutatedHash != updatedBlobHash {
		t.Fatalf("expected mutated index hash %q, got %q", updatedBlobHash, mutatedHash)
	}

	if err := repo.ReadTree(treeHash); err != nil {
		t.Fatalf("read tree: %v", err)
	}
	restoredHash, err := repo.GetIndexEntry("nested/file.txt")
	if err != nil {
		t.Fatalf("get index entry after read-tree: %v", err)
	}
	if restoredHash != blobHash {
		t.Fatalf("expected restored index hash %q, got %q", blobHash, restoredHash)
	}
}

func TestDiffFilesWithPathRewritesHeaders(t *testing.T) {
	repoRoot := createMainRepo(t)
	repo := NewShadowRepo(filepath.Join(repoRoot, ".carya"), repoRoot)
	if err := repo.Initialize(); err != nil {
		t.Fatalf("initialize shadow repo: %v", err)
	}

	work := t.TempDir()
	oldPath := filepath.Join(work, "before.txt")
	newPath := filepath.Join(work, "after.txt")
	if err := os.WriteFile(oldPath, []byte("one\n"), 0644); err != nil {
		t.Fatalf("write old file: %v", err)
	}
	if err := os.WriteFile(newPath, []byte("one\ntwo\n"), 0644); err != nil {
		t.Fatalf("write new file: %v", err)
	}

	diff, err := repo.DiffFilesWithPath(oldPath, newPath, "src/renamed.txt")
	if err != nil {
		t.Fatalf("diff files with path: %v", err)
	}
	if !strings.Contains(diff, "a/src/renamed.txt") || !strings.Contains(diff, "b/src/renamed.txt") {
		t.Fatalf("expected rewritten diff headers, got:\n%s", diff)
	}
	if !strings.Contains(diff, "+two") {
		t.Fatalf("expected content change in diff, got:\n%s", diff)
	}
}
