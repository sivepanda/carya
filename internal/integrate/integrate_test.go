package integrate

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"carya/internal/git"

	"github.com/sivepanda/mycelia"
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

func write(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for p, c := range files {
		if err := os.WriteFile(filepath.Join(dir, p), []byte(c), 0644); err != nil {
			t.Fatal(err)
		}
	}
}

func commit(t *testing.T, dir, msg string) {
	t.Helper()
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "-c", "user.name=T", "-c", "user.email=t@e", "commit", "-q", "-m", msg)
}

// agentState publishes the working tree of a fresh worktree on main, after
// applying files, as user's state.
func agentState(t *testing.T, repo, user string, files map[string]string) {
	t.Helper()
	wt := filepath.Join(t.TempDir(), user)
	gitRun(t, repo, "worktree", "add", "-q", "--detach", wt, "main")
	write(t, wt, files)
	tree, err := git.WorkingStateTree(wt)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := git.NewRefManager(wt).PublishUserState(user, tree); err != nil {
		t.Fatal(err)
	}
}

func setupRepo(t *testing.T) string {
	t.Helper()
	mycelia.ConfigFile = "carya.json"
	repo := t.TempDir()
	gitRun(t, repo, "init", "-q", "-b", "main")
	write(t, repo, map[string]string{
		"a.txt":      "a1\na2\n",
		"b.txt":      "b1\n",
		"c.txt":      "c1\n",
		"carya.json": `{"version":"1.0","post-pull":[],"post-checkout":[{"command":"touch setup-ran","description":"setup"}]}`,
	})
	commit(t, repo, "init")

	agentState(t, repo, "alice", map[string]string{"a.txt": "alice\na2\n"})
	agentState(t, repo, "bob", map[string]string{"b.txt": "bob\n"})
	agentState(t, repo, "carol", map[string]string{"a.txt": "carol\na2\n"})

	// main moves on after the agents started.
	write(t, repo, map[string]string{"c.txt": "upstream\n"})
	commit(t, repo, "upstream change")
	return repo
}

func names(merged []Merged) string {
	var n []string
	for _, m := range merged {
		n = append(n, m.UserID)
	}
	return strings.Join(n, ",")
}

func TestMergeFoldsStatesAndReportsConflicts(t *testing.T) {
	repo := setupRepo(t)

	res, err := Merge(repo, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Onto != "main" {
		t.Fatalf("onto = %q, want main", res.Onto)
	}
	if names(res.Merged) != "alice,bob" {
		t.Fatalf("merged = %s, want alice,bob", names(res.Merged))
	}
	if len(res.Conflicts) != 1 {
		t.Fatalf("want one conflict, got %+v", res.Conflicts)
	}
	c := res.Conflicts[0]
	if c.UserID != "carol" || strings.Join(c.Files, ",") != "a.txt" || strings.Join(c.With, ",") != "alice" {
		t.Fatalf("unexpected conflict: %+v", c)
	}

	show := func(path string) string { return gitRun(t, repo, "show", IntegrationRef+":"+path) }
	if show("a.txt") != "alice\na2" || show("b.txt") != "bob" || show("c.txt") != "upstream" {
		t.Fatalf("merged tree is wrong: a=%q b=%q c=%q", show("a.txt"), show("b.txt"), show("c.txt"))
	}
	if gitRun(t, repo, "status", "--porcelain") != "" {
		t.Fatal("merging touched the checkout")
	}

	only, err := Merge(repo, Options{Exclude: []string{"alice"}})
	if err != nil {
		t.Fatal(err)
	}
	if names(only.Merged) != "bob,carol" || len(only.Conflicts) != 0 {
		t.Fatalf("without alice, carol should merge: %+v", only)
	}
}

func TestCheckRunsSetupAndChecksInATemporaryWorktree(t *testing.T) {
	repo := setupRepo(t)
	worktreesBefore := gitRun(t, repo, "worktree", "list")

	res, err := Merge(repo, Options{Exclude: []string{"carol"}})
	if err != nil {
		t.Fatal(err)
	}
	opts := Options{Setup: true, Run: []string{
		"test -f setup-ran",
		"grep -q alice a.txt && grep -q bob b.txt && grep -q upstream c.txt",
	}}
	logPath := filepath.Join(t.TempDir(), "integration.log")
	if err := Check(repo, res, opts, logPath); err != nil {
		t.Fatal(err)
	}
	if !res.OK || len(res.Steps) != 3 || res.Steps[0].Kind != "setup" {
		t.Fatalf("expected setup plus two passing checks: %+v", res.Steps)
	}
	if got := gitRun(t, repo, "worktree", "list"); got != worktreesBefore {
		t.Fatalf("temporary worktree left behind:\n%s", got)
	}
	if log, _ := os.ReadFile(logPath); !strings.Contains(string(log), "$ grep -q alice") {
		t.Fatalf("log is missing the commands:\n%s", log)
	}
}

func TestCheckStopsAtTheFirstFailure(t *testing.T) {
	repo := setupRepo(t)
	res, err := Merge(repo, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := Check(repo, res, Options{Run: []string{"echo broken >&2; exit 3", "echo never"}, Keep: true}, ""); err != nil {
		t.Fatal(err)
	}
	failed := res.Failed()
	if res.OK || failed == nil || failed.ExitCode != 3 || !strings.Contains(failed.Output, "broken") || len(res.Steps) != 1 {
		t.Fatalf("expected the first check to fail and stop the run: %+v", res.Steps)
	}
	if _, err := os.Stat(filepath.Join(res.Worktree, "a.txt")); err != nil {
		t.Fatalf("--keep should leave the worktree: %v", err)
	}
	gitRun(t, repo, "worktree", "remove", "--force", res.Worktree)
}

func TestLegacyAndEmptyStatesAreSkipped(t *testing.T) {
	repo := setupRepo(t)
	tree := gitRun(t, repo, "rev-parse", "main^{tree}")
	gitRun(t, repo, "update-ref", git.UserTreeRefPath("old"), tree)
	if _, err := git.NewRefManager(repo).PublishUserState("idle", tree); err != nil {
		t.Fatal(err)
	}

	res, err := Merge(repo, Options{Only: []string{"old", "idle", "bob"}})
	if err != nil {
		t.Fatal(err)
	}
	reasons := map[string]string{}
	for _, s := range res.Skipped {
		reasons[s.UserID] = s.Reason
	}
	if !strings.Contains(reasons["old"], "older carya") || !strings.Contains(reasons["idle"], "nothing") || names(res.Merged) != "bob" {
		t.Fatalf("unexpected skips %v / merged %s", reasons, names(res.Merged))
	}
}

func TestDetectChecks(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, map[string]string{
		"go.mod":         "module x\n",
		"package.json":   `{"scripts":{"test":"vitest","lint":"eslint ."}}`,
		"pnpm-lock.yaml": "",
	})
	got := strings.Join(DetectChecks(dir), "|")
	if got != "go build ./...|go test ./...|pnpm test" {
		t.Fatalf("DetectChecks = %s", got)
	}
	if len(DetectChecks(t.TempDir())) != 0 {
		t.Fatal("an empty repo has nothing to run")
	}
}

func TestSinceLeavesOutStaleStates(t *testing.T) {
	repo := setupRepo(t)
	// Back-date bob's state by rewriting it with an old committer date.
	tree := gitRun(t, repo, "rev-parse", git.UserTreeRefPath("bob")+"^{tree}")
	base := gitRun(t, repo, "rev-parse", git.UserTreeRefPath("bob")+"^")
	cmd := exec.Command("git", "commit-tree", tree, "-p", base, "-m", "old state")
	cmd.Dir = repo
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=c", "GIT_AUTHOR_EMAIL=c@i", "GIT_COMMITTER_NAME=c", "GIT_COMMITTER_EMAIL=c@i",
		"GIT_COMMITTER_DATE=2000-01-01T00:00:00Z", "GIT_AUTHOR_DATE=2000-01-01T00:00:00Z")
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "update-ref", git.UserTreeRefPath("bob"), strings.TrimSpace(string(out)))

	res, err := Merge(repo, Options{Since: 24 * time.Hour, Exclude: []string{"carol"}})
	if err != nil {
		t.Fatal(err)
	}
	if names(res.Merged) != "alice" || len(res.Skipped) != 1 || !strings.Contains(res.Skipped[0].Reason, "older than --since") {
		t.Fatalf("bob's stale state should be left out: merged=%s skipped=%+v", names(res.Merged), res.Skipped)
	}
}
