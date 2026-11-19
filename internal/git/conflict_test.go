package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPredictionFromTreesPreservesRepositoryState(t *testing.T) {
	for _, same := range []bool{false, true} {
		name := "conflict"
		if same {
			name = "identical"
		}
		t.Run(name, func(t *testing.T) {
			repo := createMainRepo(t)
			base := strings.TrimSpace(runGit(t, repo, "rev-parse", "HEAD^{tree}"))
			tree := func(text string) string {
				t.Helper()
				if err := os.WriteFile(filepath.Join(repo, "tracked.txt"), []byte(text), 0644); err != nil {
					t.Fatal(err)
				}
				runGit(t, repo, "add", "tracked.txt")
				return strings.TrimSpace(runGit(t, repo, "write-tree"))
			}
			left := tree("Alice\n")
			right := left
			if !same {
				right = tree("Bob\n")
			}
			indexBefore, err := os.ReadFile(filepath.Join(repo, ".git/index"))
			if err != nil {
				t.Fatal(err)
			}
			refs := runGit(t, repo, "show-ref")
			status := runGit(t, repo, "status", "--porcelain")
			report, err := NewConflictPredictor(repo).PredictConflicts(base, left, right)
			if err != nil {
				t.Fatal(err)
			}
			if report.HasConflicts == same {
				t.Fatalf("unexpected report: %+v", report)
			}
			if !same && (len(report.ConflictedFiles) != 1 || report.ConflictedFiles[0].Path != "tracked.txt") {
				t.Fatalf("missing path: %+v", report)
			}
			indexAfter, err := os.ReadFile(filepath.Join(repo, ".git/index"))
			if err != nil {
				t.Fatal(err)
			}
			if string(indexBefore) != string(indexAfter) || refs != runGit(t, repo, "show-ref") || status != runGit(t, repo, "status", "--porcelain") {
				t.Fatal("prediction mutated repository state")
			}
		})
	}
}

func TestDiffTreesKeepsUnusualPaths(t *testing.T) {
	repo := createMainRepo(t)
	base := strings.TrimSpace(runGit(t, repo, "rev-parse", "HEAD^{tree}"))
	tree := treeWith(t, repo, "HEAD", map[string]string{
		"with space.txt": "x\n",
		"naïve.txt":      "y\n",
		"tracked.txt":    "changed\n",
	})
	diffs, err := NewConflictPredictor(repo).DiffTrees(base, tree)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, d := range diffs {
		got[d.Path] = d.Status
	}
	if got["with space.txt"] != "A" || got["naïve.txt"] != "A" || got["tracked.txt"] != "M" || len(got) != 3 {
		t.Fatalf("DiffTrees = %+v", diffs)
	}
}
