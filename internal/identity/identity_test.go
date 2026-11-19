package identity

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"testing"
)

func run(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// isolate keeps the test away from the developer's git and carya config, so
// every checkout derives its identity from the same user and device.
func isolate(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("USER", "tester")
	t.Setenv(AgentEnvVar, "")
}

func newRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	run(t, repo, "init", "-q")
	if err := os.WriteFile(filepath.Join(repo, "f.txt"), []byte("x\n"), 0644); err != nil {
		t.Fatal(err)
	}
	run(t, repo, "add", "f.txt")
	run(t, repo, "-c", "user.name=T", "-c", "user.email=t@e", "commit", "-q", "-m", "init")
	return repo
}

func idFor(t *testing.T, checkout string) string {
	t.Helper()
	id, err := NewUserIdentity(filepath.Join(checkout, ".carya")).GetOrCreate()
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateUserID(id); err != nil {
		t.Fatalf("generated invalid ID %q: %v", id, err)
	}
	return id
}

// Two worktrees initialized before either publishes used to derive the same
// ID from user@host and overwrite each other's state ref.
func TestWorktreesGetDistinctIdentities(t *testing.T) {
	isolate(t)
	repo := newRepo(t)
	worktree := filepath.Join(t.TempDir(), "wt")
	run(t, repo, "worktree", "add", "-q", worktree)

	main := idFor(t, repo)
	other := idFor(t, worktree)
	if main == other {
		t.Fatalf("both worktrees got identity %q", main)
	}
	if again := idFor(t, worktree); again != other {
		t.Fatalf("identity not stable: %q then %q", other, again)
	}
}

// Identical sandboxes share user name and hostname and never see each
// other's worktrees, so agents need randomness to stay distinct.
func TestAgentIdentitiesAreUniquePerCheckout(t *testing.T) {
	isolate(t)
	t.Setenv(AgentEnvVar, "Claude Code")

	first := idFor(t, newRepo(t))
	second := idFor(t, newRepo(t))
	shape := regexp.MustCompile(`^claude-code-[0-9a-f]{8}$`)
	if !shape.MatchString(first) || !shape.MatchString(second) {
		t.Fatalf("unexpected agent IDs %q, %q", first, second)
	}
	if first == second {
		t.Fatalf("two sandboxes got the same agent ID %q", first)
	}
}

func TestExistingIdentityWins(t *testing.T) {
	isolate(t)
	repo := newRepo(t)
	u := NewUserIdentity(filepath.Join(repo, ".carya"))
	if err := u.Save("alice"); err != nil {
		t.Fatal(err)
	}
	t.Setenv(AgentEnvVar, "claude")
	if id := idFor(t, repo); id != "alice" {
		t.Fatalf("existing identity replaced: got %q", id)
	}
}

func TestAgentNameIsTruncatedToAValidID(t *testing.T) {
	isolate(t)
	t.Setenv(AgentEnvVar, "a-very-long-agent-name-that-goes-on-and-on")
	id := idFor(t, newRepo(t))
	if len(id) > 33 {
		t.Fatalf("agent ID too long: %q", id)
	}
}
