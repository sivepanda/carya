package chunk

import (
	"carya/internal/git"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func gitCmd(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(out))
	}
	return string(out)
}

func makeShadowReadyRepo(t *testing.T) (string, *git.ShadowRepo) {
	t.Helper()
	repo := t.TempDir()
	gitCmd(t, repo, "init")
	if err := os.WriteFile(filepath.Join(repo, "tracked.txt"), []byte("base\n"), 0644); err != nil {
		t.Fatalf("write tracked file: %v", err)
	}
	gitCmd(t, repo, "add", "tracked.txt")
	gitCmd(t, repo, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "initial")

	shadow := git.NewShadowRepo(filepath.Join(repo, ".carya"), repo)
	if err := shadow.Initialize(); err != nil {
		t.Fatalf("initialize shadow repo: %v", err)
	}
	if err := shadow.SeedIndexFromMainHEAD(); err != nil {
		t.Fatalf("seed shadow index: %v", err)
	}

	return repo, shadow
}

func TestUnifiedStrategyOnFileChangeAndForceFlush(t *testing.T) {
	_, shadow := makeShadowReadyRepo(t)
	s := NewUnifiedStrategy(shadow)
	now := time.Now()

	s.OnFileChange(FileChangeEvent{Path: "tracked.txt", Contents: []byte("base\nnext\n"), Time: now})
	chunk := s.ForceFlush("tracked.txt")
	if chunk == nil {
		t.Fatal("expected non-nil chunk after tracked file change")
	}
	if !chunk.Manual {
		t.Fatal("expected force-flushed chunk to be marked manual")
	}
	if !strings.Contains(chunk.Diff, "tracked.txt") {
		t.Fatalf("expected diff to include tracked path, got:\n%s", chunk.Diff)
	}
	if !strings.Contains(chunk.Diff, "+next") {
		t.Fatalf("expected diff to include inserted line, got:\n%s", chunk.Diff)
	}
}

func TestUnifiedStrategyRevertDropsNetNoChange(t *testing.T) {
	_, shadow := makeShadowReadyRepo(t)
	s := NewUnifiedStrategy(shadow)
	now := time.Now()

	s.OnFileChange(FileChangeEvent{Path: "tracked.txt", Contents: []byte("base\nnext\n"), Time: now})
	s.OnFileChange(FileChangeEvent{Path: "tracked.txt", Contents: []byte("base\n"), Time: now.Add(time.Second)})

	if got := s.ForceFlush("tracked.txt"); got != nil {
		t.Fatalf("expected no chunk after net-zero revert, got %+v", *got)
	}
}

func TestUnifiedStrategyDeleteFromTrackedFile(t *testing.T) {
	_, shadow := makeShadowReadyRepo(t)
	s := NewUnifiedStrategy(shadow)
	now := time.Now()

	s.OnFileChange(FileChangeEvent{Path: "tracked.txt", Contents: nil, Time: now})
	chunk := s.ForceFlush("tracked.txt")
	if chunk == nil {
		t.Fatal("expected deletion chunk for tracked file")
	}
	if strings.TrimSpace(chunk.Diff) == "" {
		t.Fatal("expected non-empty deletion diff")
	}
}

func TestUnifiedStrategyTracksNewUnversionedFile(t *testing.T) {
	_, shadow := makeShadowReadyRepo(t)
	s := NewUnifiedStrategy(shadow)
	now := time.Now()

	s.OnFileChange(FileChangeEvent{Path: "new.txt", Contents: []byte("hello\n"), Time: now})
	chunk := s.ForceFlush("new.txt")
	if chunk == nil {
		t.Fatal("expected chunk for newly created unversioned file")
	}
	if !strings.Contains(chunk.Diff, "new.txt") {
		t.Fatalf("expected diff to include new file path, got:\n%s", chunk.Diff)
	}
	if !strings.Contains(chunk.Diff, "+hello") {
		t.Fatalf("expected diff to include inserted file contents, got:\n%s", chunk.Diff)
	}
}

func TestHelpersBinaryAndShortHash(t *testing.T) {
	if shortHash("") != "00000000" {
		t.Fatalf("unexpected short hash for empty")
	}
	if shortHash("abc") != "abc" {
		t.Fatalf("unexpected short hash for short input")
	}
	if shortHash("1234567890") != "12345678" {
		t.Fatalf("unexpected short hash truncation")
	}

	if isBinary([]byte("plain\ntext\n")) {
		t.Fatal("expected plain text not to be binary")
	}
	if !isBinary([]byte{0, 1, 2, 3}) {
		t.Fatal("expected null-byte content to be binary")
	}
}
