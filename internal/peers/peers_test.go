package peers

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"carya/internal/git"
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

func write(t *testing.T, repo string, files map[string]string) {
	t.Helper()
	for path, content := range files {
		if err := os.WriteFile(filepath.Join(repo, path), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
}

// publish writes files into the working tree and publishes it as user's state.
func publish(t *testing.T, repo, user string, files map[string]string) git.UserRef {
	t.Helper()
	write(t, repo, files)
	tree, err := git.WorkingStateTree(repo)
	if err != nil {
		t.Fatal(err)
	}
	state, err := git.NewRefManager(repo).PublishUserState(user, tree)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func TestForPathsReportsPeerChangesAndConflicts(t *testing.T) {
	repo := t.TempDir()
	gitRun(t, repo, "init", "-q")
	write(t, repo, map[string]string{"shared.txt": "shared\n", "solo.txt": "solo\n"})
	gitRun(t, repo, "add", "-A")
	gitRun(t, repo, "-c", "user.name=T", "-c", "user.email=t@e", "commit", "-q", "-m", "init")

	publish(t, repo, "bob", map[string]string{"shared.txt": "bob\n"})
	gitRun(t, repo, "checkout", "-q", "--", ".")
	mine := publish(t, repo, "me", map[string]string{"shared.txt": "me\n", "solo.txt": "mine\n"})

	c := NewChecker(repo)
	changes, err := c.ForPaths("me", mine, []string{"shared.txt", "solo.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 {
		t.Fatalf("want only bob's change to shared.txt, got %+v", changes)
	}
	bob := changes[0]
	if bob.UserID != "bob" || bob.Path != "shared.txt" || bob.Status != "M" || !bob.Conflict {
		t.Fatalf("unexpected change: %+v", bob)
	}
	if !strings.Contains(bob.Patch, "+bob") {
		t.Fatalf("patch should be bob's edit:\n%s", bob.Patch)
	}
	if bob.Published.IsZero() {
		t.Fatal("publish time missing")
	}

	summary := Summary(changes, time.Now())
	if !strings.Contains(summary, "bob has unfinished work that changed shared.txt") || !strings.Contains(summary, "CONFLICT") {
		t.Fatalf("unexpected summary:\n%s", summary)
	}

	// Without a state of my own there is nothing to conflict with.
	changes, err = c.ForPaths("me", git.UserRef{}, []string{"shared.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].Conflict {
		t.Fatalf("expected a non-conflicting report, got %+v", changes)
	}

	if got, _ := c.ForPaths("me", mine, []string{"solo.txt"}); len(got) != 0 {
		t.Fatalf("nobody else touched solo.txt, got %+v", got)
	}
	if Summary(nil, time.Now()) != "" {
		t.Fatal("empty summary expected")
	}
}
