package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// treeWith builds a tree from base with files overwritten, using a throwaway
// index so the repository's own index is untouched.
func treeWith(t *testing.T, repo, base string, files map[string]string) string {
	t.Helper()
	env := append(os.Environ(), "GIT_INDEX_FILE="+filepath.Join(t.TempDir(), "index"))
	run := func(stdin string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		cmd.Env = env
		cmd.Stdin = strings.NewReader(stdin)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	run("", "read-tree", base)
	for path, content := range files {
		blob := run(content, "hash-object", "-w", "--stdin")
		run("", "update-index", "--add", "--cacheinfo", "100644,"+blob+","+path)
	}
	return run("", "write-tree")
}

func commitFiles(t *testing.T, repo string, files map[string]string, msg string) string {
	t.Helper()
	for path, content := range files {
		if err := os.WriteFile(filepath.Join(repo, path), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
		runGit(t, repo, "add", path)
	}
	runGit(t, repo, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-q", "-m", msg)
	return strings.TrimSpace(runGit(t, repo, "rev-parse", "HEAD"))
}

func TestPublishUserStateRecordsHEADAsBase(t *testing.T) {
	repo := createMainRepo(t)
	refs := NewRefManager(repo)
	first := strings.TrimSpace(runGit(t, repo, "rev-parse", "HEAD"))
	tree := treeWith(t, repo, "HEAD", map[string]string{"tracked.txt": "edited\n"})

	state, err := refs.PublishUserState("alice", tree)
	if err != nil {
		t.Fatal(err)
	}
	if !state.HasBase() || state.BaseCommit != first || state.TreeHash != tree {
		t.Fatalf("unexpected state: %+v", state)
	}
	if got := strings.TrimSpace(runGit(t, repo, "cat-file", "-t", UserTreeRefPath("alice"))); got != "commit" {
		t.Fatalf("state ref points at a %s, want a commit", got)
	}
	if got := strings.TrimSpace(runGit(t, repo, "rev-parse", UserTreeRefPath("alice")+"^")); got != first {
		t.Fatalf("state parent = %s, want HEAD %s", got, first)
	}

	again, err := refs.PublishUserState("alice", tree)
	if err != nil {
		t.Fatal(err)
	}
	if again.Commit != state.Commit {
		t.Fatalf("republishing an unchanged state made a new commit: %s then %s", state.Commit, again.Commit)
	}

	second := commitFiles(t, repo, map[string]string{"other.txt": "x\n"}, "second")
	moved, err := refs.PublishUserState("alice", tree)
	if err != nil {
		t.Fatal(err)
	}
	if moved.BaseCommit != second || moved.Commit == state.Commit {
		t.Fatalf("state did not follow the new HEAD: %+v", moved)
	}

	read, err := refs.GetUserState("alice")
	if err != nil {
		t.Fatal(err)
	}
	if read != moved {
		t.Fatalf("GetUserState = %+v, want %+v", read, moved)
	}
}

func TestLegacyStateRefsHaveNoBase(t *testing.T) {
	repo := createMainRepo(t)
	refs := NewRefManager(repo)
	tree := strings.TrimSpace(runGit(t, repo, "rev-parse", "HEAD^{tree}"))
	parentless := strings.TrimSpace(runGit(t, repo, "-c", "user.name=T", "-c", "user.email=t@e", "commit-tree", tree, "-m", "state snapshot"))
	runGit(t, repo, "update-ref", UserTreeRefPath("bare"), tree)
	runGit(t, repo, "update-ref", UserTreeRefPath("pushed"), parentless)

	for _, user := range []string{"bare", "pushed"} {
		state, err := refs.GetUserState(user)
		if err != nil {
			t.Fatalf("%s: %v", user, err)
		}
		if state.HasBase() || state.TreeHash != tree || state.Base("fallback") != "fallback" {
			t.Fatalf("%s: unexpected state %+v", user, state)
		}
	}

	listed, err := refs.ListUserRefs()
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 2 {
		t.Fatalf("ListUserRefs = %+v, want both legacy refs", listed)
	}
}

func TestPushUserRefSendsStateWithItsBase(t *testing.T) {
	repo := createMainRepo(t)
	remote := t.TempDir()
	runGit(t, remote, "init", "-q", "--bare")
	runGit(t, repo, "remote", "add", "origin", remote)

	// The base is a local commit the remote has never seen.
	base := commitFiles(t, repo, map[string]string{"local.txt": "unpushed\n"}, "local only")
	refs := NewRefManager(repo)
	tree := treeWith(t, repo, "HEAD", map[string]string{"tracked.txt": "wip\n"})
	state, err := refs.PublishUserState("alice", tree)
	if err != nil {
		t.Fatal(err)
	}
	if err := refs.PushUserRef("origin", "alice"); err != nil {
		t.Fatal(err)
	}

	pushed := strings.Fields(runGit(t, remote, "show", "-s", "--format=%H %P", UserTreeRefPath("alice")))
	if len(pushed) != 2 || pushed[0] != state.Commit || pushed[1] != base {
		t.Fatalf("remote state = %v, want commit %s on base %s", pushed, state.Commit, base)
	}
	runGit(t, remote, "cat-file", "-e", base+"^{commit}")
}

// Two states built on different commits must be compared through their own
// bases. With HEAD as everyone's base, an upstream change the teammate does
// not have yet looks like the teammate reverting it, and conflicts with any
// local edit near it.
func TestPredictStateConflictsUsesEachStatesBase(t *testing.T) {
	repo := createMainRepo(t)
	commitFiles(t, repo, map[string]string{
		"a.txt":      "one\ntwo\nthree\n",
		"shared.txt": "shared\n",
	}, "c1")
	refs := NewRefManager(repo)

	bobTree := treeWith(t, repo, "HEAD", map[string]string{"shared.txt": "bob\n"})
	bob, err := refs.PublishUserState("bob", bobTree)
	if err != nil {
		t.Fatal(err)
	}

	head := commitFiles(t, repo, map[string]string{"a.txt": "one\nTWO\nthree\n"}, "c2 upstream")
	myTree := treeWith(t, repo, "HEAD", map[string]string{
		"a.txt":      "one\nTWO!\nthree\n",
		"shared.txt": "me\n",
	})
	mine, err := refs.PublishUserState("me", myTree)
	if err != nil {
		t.Fatal(err)
	}

	predictor := NewConflictPredictor(repo)

	// The old approach: HEAD's tree as the base for both sides.
	old, err := predictor.PredictConflicts(head, mine.TreeHash, bob.TreeHash)
	if err != nil {
		t.Fatal(err)
	}
	if !conflictedPaths(old)["a.txt"] {
		t.Fatalf("expected the HEAD-as-base prediction to flag a.txt, got %+v", old.ConflictedFiles)
	}

	report, err := predictor.PredictStateConflicts(mine, bob, head)
	if err != nil {
		t.Fatal(err)
	}
	got := conflictedPaths(report)
	if len(got) != 1 || !got["shared.txt"] {
		t.Fatalf("conflicts = %+v, want only shared.txt", report.ConflictedFiles)
	}

	overlap, err := predictor.OverlapsWith(mine.Base(head), mine.TreeHash, bob.Base(head), bob.TreeHash)
	if err != nil {
		t.Fatal(err)
	}
	if len(overlap) != 1 || overlap[0] != "shared.txt" {
		t.Fatalf("overlap = %v, want [shared.txt]", overlap)
	}

	// A legacy state with no base falls back to the given base.
	legacy := UserRef{UserID: "old", TreeHash: bobTree}
	if _, err := predictor.PredictStateConflicts(mine, legacy, head); err != nil {
		t.Fatalf("legacy fallback: %v", err)
	}
	if _, err := predictor.PredictStateConflicts(mine, legacy, ""); err == nil {
		t.Fatal("expected an error for a legacy state without a fallback base")
	}
}

func conflictedPaths(r *ConflictReport) map[string]bool {
	paths := make(map[string]bool)
	for _, f := range r.ConflictedFiles {
		paths[f.Path] = true
	}
	return paths
}
