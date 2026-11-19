package states

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
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

func write(t *testing.T, dir, path, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, path), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func commitAll(t *testing.T, dir, msg string) {
	t.Helper()
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "-c", "user.name=T", "-c", "user.email=t@e", "commit", "-q", "-m", msg)
}

// publish writes files over HEAD's tree and publishes it as user's state.
func publish(t *testing.T, repo, user string, files map[string]string) {
	t.Helper()
	wt := filepath.Join(t.TempDir(), user)
	gitRun(t, repo, "worktree", "add", "-q", "--detach", wt, "HEAD")
	for p, c := range files {
		write(t, wt, p, c)
	}
	tree, err := git.WorkingStateTree(wt)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := git.NewRefManager(wt).PublishUserState(user, tree); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "worktree", "remove", "--force", wt)
}

func backdate(t *testing.T, repo, user string) {
	t.Helper()
	redate(t, repo, user, time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC))
}

// redate republishes user's state with the same tree and base but published
// at when. Commit times have one-second resolution, so tests that care about
// the order of states set it explicitly instead of relying on the clock.
func redate(t *testing.T, repo, user string, when time.Time) {
	t.Helper()
	ref := git.UserTreeRefPath(user)
	tree := gitRun(t, repo, "rev-parse", ref+"^{tree}")
	base := gitRun(t, repo, "rev-parse", ref+"^")
	date := when.UTC().Format(time.RFC3339)
	cmd := exec.Command("git", "commit-tree", tree, "-p", base, "-m", "redated")
	cmd.Dir = repo
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=c", "GIT_AUTHOR_EMAIL=c@i", "GIT_COMMITTER_NAME=c",
		"GIT_COMMITTER_EMAIL=c@i", "GIT_AUTHOR_DATE="+date, "GIT_COMMITTER_DATE="+date)
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "update-ref", ref, strings.TrimSpace(string(out)))
}

func setup(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	gitRun(t, repo, "init", "-q", "-b", "main")
	write(t, repo, "a.txt", "a\n")
	write(t, repo, "b.txt", "b\n")
	commitAll(t, repo, "init")

	publish(t, repo, "alice", map[string]string{"a.txt": "alice\n"})
	publish(t, repo, "bob", map[string]string{"b.txt": "bob\n"})
	// Alice is the newest state. Pin both times: published back to back,
	// they may or may not fall in the same second.
	now := time.Now()
	redate(t, repo, "bob", now.Add(-2*time.Minute))
	redate(t, repo, "alice", now.Add(-time.Minute))
	publish(t, repo, "old", map[string]string{"a.txt": "old\n"})
	backdate(t, repo, "old")
	publish(t, repo, "me", map[string]string{"a.txt": "me\n"})
	backdate(t, repo, "me")

	// Bob's work lands on main.
	write(t, repo, "b.txt", "bob\n")
	commitAll(t, repo, "land bob")
	return repo
}

func ids(cs []Candidate) string {
	var out []string
	for _, c := range cs {
		out = append(out, c.UserID+":"+c.Reason)
	}
	sort.Strings(out)
	return strings.Join(out, ",")
}

func TestSelectPicksLandedAndOldStates(t *testing.T) {
	repo := setup(t)
	all, err := List(repo, "me", "main")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 4 || all[0].UserID != "alice" {
		t.Fatalf("want four states, newest first: %+v", all)
	}
	for _, st := range all {
		if st.UserID == "bob" && !st.Landed {
			t.Fatal("bob's work is on main, so his state has landed")
		}
		if st.UserID == "alice" && (st.Landed || st.Files != 1 || !st.Local || st.OnRemote) {
			t.Fatalf("unexpected alice: %+v", st)
		}
	}

	got := ids(Select(all, PruneOptions{OlderThan: 7 * 24 * time.Hour, Landed: true}))
	if !strings.HasPrefix(got, "bob:its work has landed,old:not republished for ") || strings.Contains(got, "me:") || strings.Contains(got, "alice") {
		t.Fatalf("unexpected selection: %s", got)
	}
	if got := ids(Select(all, PruneOptions{Users: []string{"alice", "me"}, Landed: true})); got != "alice:named,me:named" {
		t.Fatalf("named users should be exactly what is pruned: %s", got)
	}
	if got := Select(all, PruneOptions{}); len(got) != 0 {
		t.Fatalf("no rules, nothing pruned: %+v", got)
	}
}

func TestDeleteLocalAndRemote(t *testing.T) {
	repo := setup(t)
	remote := t.TempDir()
	gitRun(t, remote, "init", "-q", "--bare")
	gitRun(t, repo, "remote", "add", "origin", remote)
	gitRun(t, repo, "push", "-q", "origin", "refs/carya/*:refs/carya/*")
	refs := git.NewRefManager(repo)
	if err := refs.FetchCaryaRefs("origin"); err != nil {
		t.Fatal(err)
	}

	pick := func() []Candidate {
		all, err := List(repo, "me", "main")
		if err != nil {
			t.Fatal(err)
		}
		return Select(all, PruneOptions{Users: []string{"old"}})
	}

	left, err := Delete(repo, pick(), false)
	if err != nil || left != 1 {
		t.Fatalf("local delete: left=%d err=%v", left, err)
	}
	if out := gitRun(t, repo, "for-each-ref", "--format=%(refname)"); strings.Contains(out, "/old/") {
		t.Fatalf("old's refs should be gone locally:\n%s", out)
	}
	if err := refs.FetchCaryaRefs("origin"); err != nil {
		t.Fatal(err)
	}
	if len(pick()) != 1 {
		t.Fatal("without --remote, the next fetch brings the state back")
	}

	if _, err := Delete(repo, pick(), true); err != nil {
		t.Fatal(err)
	}
	if out := gitRun(t, remote, "for-each-ref", "refs/carya/"); strings.Contains(out, "/old/") || !strings.Contains(out, "/alice/") {
		t.Fatalf("--remote should delete only old's state on origin:\n%s", out)
	}
	if err := refs.FetchCaryaRefs("origin"); err != nil {
		t.Fatal(err)
	}
	if len(pick()) != 0 {
		t.Fatal("after deleting on origin it stays gone")
	}
}
