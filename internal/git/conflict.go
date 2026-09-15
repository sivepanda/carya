package git

import (
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
	cmd := exec.Command("git", "merge-tree", "--write-tree", "--merge-base", base, treeA, treeB)
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
		return nil, err
	}

	lines := strings.Split(strings.TrimSpace(outputStr), "\n")
	if len(lines) > 0 {
		report.MergedTreeHash = lines[0]
	}

	return report, nil
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

func (c *ConflictPredictor) OverlapsWith(base, treeA, treeB string) ([]string, error) {
	changesA, err := c.DiffTrees(base, treeA)
	if err != nil {
		return nil, err
	}

	changesB, err := c.DiffTrees(base, treeB)
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
