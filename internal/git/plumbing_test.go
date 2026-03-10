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

func TestAppendAlternateIsIdempotent(t *testing.T) {
	altFile := filepath.Join(t.TempDir(), "objects", "info", "alternates")
	target := "/tmp/objects-path"

	if err := appendAlternate(altFile, target); err != nil {
		t.Fatalf("first appendAlternate failed: %v", err)
	}
	if err := appendAlternate(altFile, target); err != nil {
		t.Fatalf("second appendAlternate failed: %v", err)
	}

	b, err := os.ReadFile(altFile)
	if err != nil {
		t.Fatalf("read alternates file: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) != 1 || lines[0] != target {
		t.Fatalf("expected single alternate %q, got %q", target, lines)
	}
}

func TestShadowRepoInitializeAndAlternates(t *testing.T) {
	repoRoot := createMainRepo(t)
	caryaPath := filepath.Join(repoRoot, ".carya")
	repo := NewShadowRepo(caryaPath, repoRoot)

	if err := repo.Initialize(); err != nil {
		t.Fatalf("initialize shadow repo: %v", err)
	}

	if _, err := os.Stat(filepath.Join(repo.GitDir(), "HEAD")); err != nil {
		t.Fatalf("expected bare repo HEAD file: %v", err)
	}

	mainAlt := filepath.Join(repoRoot, ".git", "objects", "info", "alternates")
	shadowAlt := filepath.Join(repo.GitDir(), "objects", "info", "alternates")

	mainAltData, err := os.ReadFile(mainAlt)
	if err != nil {
		t.Fatalf("read main alternates: %v", err)
	}
	shadowAltData, err := os.ReadFile(shadowAlt)
	if err != nil {
		t.Fatalf("read shadow alternates: %v", err)
	}

	shadowObjects := filepath.Join(repo.GitDir(), "objects")
	mainObjects := filepath.Join(repoRoot, ".git", "objects")
	if !strings.Contains(string(mainAltData), shadowObjects) {
		t.Fatalf("main alternates does not contain shadow objects path: %q", shadowObjects)
	}
	if !strings.Contains(string(shadowAltData), mainObjects) {
		t.Fatalf("shadow alternates does not contain main objects path: %q", mainObjects)
	}
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
