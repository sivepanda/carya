package agent

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func isolate(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("CARYA_AGENT", "")
	t.Setenv("CLAUDE_CODE_REMOTE", "")
}

func newRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	gitRun(t, repo, "init", "-q")
	if err := os.WriteFile(filepath.Join(repo, "shared.txt"), []byte("shared\n"), 0644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "add", "-A")
	gitRun(t, repo, "-c", "user.name=T", "-c", "user.email=t@e", "commit", "-q", "-m", "init")
	return repo
}

func TestBootstrapLeavesTrackedFilesAlone(t *testing.T) {
	isolate(t)
	repo := newRepo(t)

	s, err := Bootstrap(repo, "claude")
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^claude-[0-9a-f]{8}$`).MatchString(s.UserID) {
		t.Fatalf("unexpected agent identity %q", s.UserID)
	}
	if status := gitRun(t, repo, "status", "--porcelain"); status != "" {
		t.Fatalf("bootstrap left the checkout dirty:\n%s", status)
	}
	if _, err := os.Stat(filepath.Join(repo, ".gitignore")); !os.IsNotExist(err) {
		t.Fatal("bootstrap should not create or edit .gitignore")
	}

	again, err := Bootstrap(repo, "claude")
	if err != nil {
		t.Fatal(err)
	}
	if again.UserID != s.UserID {
		t.Fatalf("identity changed on second bootstrap: %q then %q", s.UserID, again.UserID)
	}
	exclude, _ := os.ReadFile(filepath.Join(repo, ".git", "info", "exclude"))
	if strings.Count(string(exclude), ".carya/") != 1 {
		t.Fatalf("info/exclude should list .carya/ once:\n%s", exclude)
	}
	if ok, why := s.Sharing(); ok || !strings.Contains(why, "origin") {
		t.Fatalf("a repo without origin should not share: %v %q", ok, why)
	}
}

// Two agents in two worktrees of one repository see each other's work
// through the shared refs, with no daemon and no push.
func TestWorktreeAgentsSeeEachOther(t *testing.T) {
	isolate(t)
	repo := newRepo(t)
	other := filepath.Join(t.TempDir(), "other")
	gitRun(t, repo, "worktree", "add", "-q", other)

	a, err := Bootstrap(repo, "claude")
	if err != nil {
		t.Fatal(err)
	}
	b, err := Bootstrap(other, "claude")
	if err != nil {
		t.Fatal(err)
	}
	if a.UserID == b.UserID {
		t.Fatalf("worktree agents share identity %q", a.UserID)
	}

	// Agent B edits through a "shell command" and its post-tool hook snapshots.
	if err := os.WriteFile(filepath.Join(other, "shared.txt"), []byte("from b\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Snapshot(); err != nil {
		t.Fatal(err)
	}

	// Agent A is about to edit the same file, by absolute path.
	changes, err := a.Check([]string{filepath.Join(repo, "shared.txt"), "/outside/the/repo.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].UserID != b.UserID || changes[0].Conflict {
		t.Fatalf("expected B's non-conflicting change, got %+v", changes)
	}

	// Once A has its own conflicting edit, the check says so.
	if err := os.WriteFile(filepath.Join(repo, "shared.txt"), []byte("from a\n"), 0644); err != nil {
		t.Fatal(err)
	}
	changes, err = a.Check([]string{"shared.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || !changes[0].Conflict {
		t.Fatalf("expected a conflict with B, got %+v", changes)
	}
}

func TestRelPathsHandlesNewFilesAndOutsiders(t *testing.T) {
	isolate(t)
	repo := newRepo(t)
	s, err := Bootstrap(repo, "claude")
	if err != nil {
		t.Fatal(err)
	}
	got := s.RelPaths([]string{
		filepath.Join(repo, "new", "file.go"),
		"rel/path.go",
		repo,
		filepath.Dir(repo),
		"",
	})
	want := []string{"new/file.go", "rel/path.go"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("RelPaths = %v, want %v", got, want)
	}
}

func TestClaimSyncNeedsARemote(t *testing.T) {
	isolate(t)
	repo := newRepo(t)
	s, err := Bootstrap(repo, "claude")
	if err != nil {
		t.Fatal(err)
	}
	if s.ClaimSync(time.Minute) {
		t.Fatal("no remote, so no sync should be claimed")
	}

	remote := t.TempDir()
	gitRun(t, remote, "init", "-q", "--bare")
	gitRun(t, repo, "remote", "add", "origin", remote)
	if !s.ClaimSync(time.Minute) {
		t.Fatal("first claim should succeed")
	}
	if s.ClaimSync(time.Minute) {
		t.Fatal("second claim inside the interval should be refused")
	}
	if err := s.Sync(); err != nil {
		t.Fatal(err)
	}
	if out := gitRun(t, remote, "for-each-ref", "refs/carya/"); !strings.Contains(out, s.UserID) {
		t.Fatalf("state was not pushed: %q", out)
	}

	t.Setenv("CLAUDE_CODE_REMOTE", "true")
	if ok, _ := s.Sharing(); ok {
		t.Fatal("cloud sessions must not share state")
	}
}
