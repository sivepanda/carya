package lsp

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"carya/internal/git"
	"carya/internal/peers"
)

func gitIn(t *testing.T, dir string, env []string, stdin string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func writeFiles(t *testing.T, repo string, files map[string]string) {
	t.Helper()
	for path, content := range files {
		if err := os.WriteFile(filepath.Join(repo, path), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
}

func commit(t *testing.T, repo string, files map[string]string, msg string) {
	t.Helper()
	writeFiles(t, repo, files)
	gitIn(t, repo, nil, "", "add", "-A")
	gitIn(t, repo, nil, "", "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-q", "-m", msg)
}

// treeOnHEAD builds HEAD's tree with files overwritten, in a throwaway index.
func treeOnHEAD(t *testing.T, repo string, files map[string]string) string {
	t.Helper()
	env := []string{"GIT_INDEX_FILE=" + filepath.Join(t.TempDir(), "index")}
	gitIn(t, repo, env, "", "read-tree", "HEAD")
	for path, content := range files {
		blob := gitIn(t, repo, env, content, "hash-object", "-w", "--stdin")
		gitIn(t, repo, env, "", "update-index", "--add", "--cacheinfo", "100644,"+blob+","+path)
	}
	return gitIn(t, repo, env, "", "write-tree")
}

func newTestAnalyzer(repo, userID string) *Analyzer {
	return &Analyzer{
		repoPath:      repo,
		userID:        userID,
		refs:          git.NewRefManager(repo),
		peers:         peers.NewChecker(repo),
		reports:       make(map[string]*FileReport),
		lastFetch:     time.Now(),
		fetchInterval: time.Hour,
	}
}

// Bob published on an older commit than the local HEAD. His report must show
// only what he changed on his own base, not upstream changes he lacks.
func TestRefreshMeasuresTeammatesAgainstTheirOwnBase(t *testing.T) {
	repo := t.TempDir()
	gitIn(t, repo, nil, "", "init", "-q")
	commit(t, repo, map[string]string{
		"a.txt":      "one\ntwo\nthree\n",
		"shared.txt": "shared\n",
	}, "c1")
	refs := git.NewRefManager(repo)

	bobFiles := map[string]string{"shared.txt": "bob\n"}
	if _, err := refs.PublishUserState("bob", treeOnHEAD(t, repo, bobFiles)); err != nil {
		t.Fatal(err)
	}

	commit(t, repo, map[string]string{"a.txt": "one\nTWO\nthree\n"}, "c2 upstream")
	mineFiles := map[string]string{
		"a.txt":      "one\nTWO!\nthree\n",
		"shared.txt": "me\n",
	}
	writeFiles(t, repo, mineFiles)
	if _, err := refs.PublishUserState("me", treeOnHEAD(t, repo, mineFiles)); err != nil {
		t.Fatal(err)
	}

	a := newTestAnalyzer(repo, "me")
	if err := a.Refresh(); err != nil {
		t.Fatal(err)
	}

	if r := a.ReportForFile("a.txt"); r != nil {
		t.Fatalf("bob never touched a.txt, got report %+v", r.Teammates)
	}

	r := a.ReportForFile("shared.txt")
	if r == nil || len(r.Teammates) != 1 {
		t.Fatalf("expected one teammate on shared.txt, got %+v", r)
	}
	bob := r.Teammates[0]
	if bob.UserID != "bob" || !bob.IsConflict {
		t.Fatalf("expected a conflict with bob, got %+v", bob)
	}
	if !strings.Contains(bob.Patch, "+bob") || strings.Contains(bob.Patch, "a.txt") {
		t.Fatalf("patch should be bob's own change to shared.txt:\n%s", bob.Patch)
	}
}

// A teammate ref from before bases were recorded is still read, using the
// local HEAD as its base as before.
func TestRefreshFallsBackToHEADForLegacyStates(t *testing.T) {
	repo := t.TempDir()
	gitIn(t, repo, nil, "", "init", "-q")
	commit(t, repo, map[string]string{"shared.txt": "shared\n"}, "c1")

	gitIn(t, repo, nil, "", "update-ref", git.UserTreeRefPath("old"), treeOnHEAD(t, repo, map[string]string{"shared.txt": "old\n"}))
	writeFiles(t, repo, map[string]string{"shared.txt": "me\n"})

	a := newTestAnalyzer(repo, "me")
	if err := a.Refresh(); err != nil {
		t.Fatal(err)
	}
	r := a.ReportForFile("shared.txt")
	if r == nil || len(r.Teammates) != 1 || r.Teammates[0].UserID != "old" {
		t.Fatalf("expected the legacy teammate on shared.txt, got %+v", r)
	}
	if r.Teammates[0].IsConflict {
		t.Fatal("no published state of mine, so no conflict should be predicted")
	}
}
