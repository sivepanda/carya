package git

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

type ConflictPredictor struct {
	repoPath string
}

type ConflictReport struct {
	HasConflicts    bool
	ConflictedFiles []ConflictedFile
	MergedTreeHash  string
	RawOutput       string
}

type ConflictedFile struct {
	Path         string
	ConflictType string // "content", "add/add", "modify/delete", "rename/delete", etc.
}

func NewConflictPredictor(repoPath string) *ConflictPredictor {
	return &ConflictPredictor{repoPath: repoPath}
}

func (c *ConflictPredictor) PredictConflicts(base, treeA, treeB string) (*ConflictReport, error) {
	// Git 2.39 supports --write-tree but not --merge-base. Unreferenced
	// commits give Git exactly the supplied base without changing any refs.
	parent, err := c.predictionCommit(base, "", "base")
	if err != nil {
		return nil, err
	}
	left, err := c.predictionCommit(treeA, parent, "left")
	if err != nil {
		return nil, err
	}
	right, err := c.predictionCommit(treeB, parent, "right")
	if err != nil {
		return nil, err
	}
	return c.mergeTree(left, right)
}

// PredictStateConflicts predicts conflicts from merging two published states.
// States that record their base are merged as the commits they are, so git
// uses the true merge base of the two bases rather than one shared guess.
// A legacy state without a base is treated as built on fallbackBase (a
// commit-ish, normally the local HEAD), matching the old behaviour.
func (c *ConflictPredictor) PredictStateConflicts(mine, theirs UserRef, fallbackBase string) (*ConflictReport, error) {
	left, err := c.stateCommit(mine, fallbackBase, "left")
	if err != nil {
		return nil, err
	}
	right, err := c.stateCommit(theirs, fallbackBase, "right")
	if err != nil {
		return nil, err
	}
	return c.mergeTree(left, right)
}

func (c *ConflictPredictor) stateCommit(state UserRef, fallbackBase, label string) (string, error) {
	if state.HasBase() {
		return state.Commit, nil
	}
	if fallbackBase == "" {
		return "", fmt.Errorf("state for %s records no base and no fallback base was given", state.UserID)
	}
	resolve := exec.Command("git", "rev-parse", "--verify", "--end-of-options", fallbackBase+"^{commit}")
	resolve.Dir = c.repoPath
	parent, err := resolve.Output()
	if err != nil {
		return "", fmt.Errorf("resolve fallback base %s: %w", fallbackBase, err)
	}
	return c.predictionCommit(state.TreeHash, strings.TrimSpace(string(parent)), label)
}

func (c *ConflictPredictor) mergeTree(left, right string) (*ConflictReport, error) {
	cmd := exec.Command("git", "merge-tree", "--write-tree", left, right)
	cmd.Dir = c.repoPath

	output, err := cmd.CombinedOutput()
	outputStr := string(output)

	report := &ConflictReport{RawOutput: outputStr}

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			report.HasConflicts = true
			report.ConflictedFiles = parseConflicts(outputStr)
			return report, nil
		}
		return nil, fmt.Errorf("predict merge: %w\n%s", err, outputStr)
	}

	lines := strings.Split(strings.TrimSpace(outputStr), "\n")
	if len(lines) > 0 {
		report.MergedTreeHash = lines[0]
	}

	return report, nil
}

func (c *ConflictPredictor) predictionCommit(treeish, parent, label string) (string, error) {
	resolve := exec.Command("git", "rev-parse", "--verify", "--end-of-options", treeish+"^{tree}")
	resolve.Dir = c.repoPath
	tree, err := resolve.Output()
	if err != nil {
		return "", fmt.Errorf("resolve prediction %s tree: %w", label, err)
	}
	args := []string{"commit-tree", strings.TrimSpace(string(tree)), "-m", "carya merge prediction " + label}
	if parent != "" {
		args = append(args, "-p", parent)
	}
	cmd := exec.Command("git", args...)
	cmd.Dir = c.repoPath
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Carya", "GIT_AUTHOR_EMAIL=carya@internal",
		"GIT_COMMITTER_NAME=Carya", "GIT_COMMITTER_EMAIL=carya@internal",
		"GIT_AUTHOR_DATE=2000-01-01T00:00:00Z", "GIT_COMMITTER_DATE=2000-01-01T00:00:00Z")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("create prediction %s commit: %w\n%s", label, err, out)
	}
	return strings.TrimSpace(string(out)), nil
}

var (
	mergeConflictRe = regexp.MustCompile(`(?i)Merge conflict in (.+)$`)
	modifyDeleteRe  = regexp.MustCompile(`(?i)CONFLICT \(modify/delete\): (.+?) deleted in`)
	renameDeleteRe  = regexp.MustCompile(`(?i)CONFLICT \(rename/delete\): (.+?) renamed`)
	conflictTypeRe  = regexp.MustCompile(`CONFLICT \(([^)]+)\)`)
)

func parseConflicts(output string) []ConflictedFile {
	seen := make(map[string]bool)
	var conflicts []ConflictedFile

	for _, line := range strings.Split(output, "\n") {
		if !strings.Contains(line, "CONFLICT") {
			continue
		}

		conflictType := "content"
		if m := conflictTypeRe.FindStringSubmatch(line); len(m) > 1 {
			conflictType = m[1]
		}

		var path string
		if m := mergeConflictRe.FindStringSubmatch(line); len(m) > 1 {
			path = m[1]
		} else if m := modifyDeleteRe.FindStringSubmatch(line); len(m) > 1 {
			path = m[1]
		} else if m := renameDeleteRe.FindStringSubmatch(line); len(m) > 1 {
			path = m[1]
		}

		if path != "" && !seen[path] {
			seen[path] = true
			conflicts = append(conflicts, ConflictedFile{
				Path:         strings.TrimSpace(path),
				ConflictType: conflictType,
			})
		}
	}

	return conflicts
}

func (c *ConflictPredictor) PredictConflictsSimple(base, treeA, treeB string) (bool, error) {
	report, err := c.PredictConflicts(base, treeA, treeB)
	if err != nil {
		return false, err
	}
	return report.HasConflicts, nil
}

func (c *ConflictPredictor) GetCommonAncestor(commitA, commitB string) (string, error) {
	cmd := exec.Command("git", "merge-base", commitA, commitB)
	cmd.Dir = c.repoPath

	output, err := cmd.Output()
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(output)), nil
}

func (c *ConflictPredictor) DiffTrees(treeA, treeB string) ([]TreeDiff, error) {
	cmd := exec.Command("git", "diff-tree", "-r", "--name-status", treeA, treeB)
	cmd.Dir = c.repoPath

	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var diffs []TreeDiff
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		diffs = append(diffs, TreeDiff{Status: parts[0], Path: parts[1]})
	}

	return diffs, nil
}

type TreeDiff struct {
	Status string // A=added, D=deleted, M=modified, R=renamed, C=copied
	Path   string
}

// OverlapsWith returns the paths that both treeA (relative to baseA) and
// treeB (relative to baseB) change. Each side is diffed against its own base
// so changes one side merely lacks are not counted as edits.
func (c *ConflictPredictor) OverlapsWith(baseA, treeA, baseB, treeB string) ([]string, error) {
	changesA, err := c.DiffTrees(baseA, treeA)
	if err != nil {
		return nil, err
	}

	changesB, err := c.DiffTrees(baseB, treeB)
	if err != nil {
		return nil, err
	}

	pathsA := make(map[string]bool)
	for _, d := range changesA {
		pathsA[d.Path] = true
	}

	var overlapping []string
	for _, d := range changesB {
		if pathsA[d.Path] {
			overlapping = append(overlapping, d.Path)
		}
	}

	return overlapping, nil
}
