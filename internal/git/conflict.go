package git

import (
	"os/exec"
	"strings"
)

// ConflictPredictor uses git merge-tree to predict conflicts between trees.
type ConflictPredictor struct {
	repoPath string
}

// ConflictReport contains the results of a conflict prediction.
type ConflictReport struct {
	HasConflicts    bool
	ConflictedFiles []ConflictedFile
	MergedTreeHash  string // Only set if no conflicts
	RawOutput       string
}

// ConflictedFile represents a file with merge conflicts.
type ConflictedFile struct {
	Path       string
	OurHash    string
	TheirHash  string
	BaseHash   string
	ConflictID string
}

// NewConflictPredictor creates a new ConflictPredictor for the given repository.
func NewConflictPredictor(repoPath string) *ConflictPredictor {
	return &ConflictPredictor{
		repoPath: repoPath,
	}
}

// PredictConflicts uses git merge-tree to predict conflicts between two trees
// relative to a common base.
func (c *ConflictPredictor) PredictConflicts(base, treeA, treeB string) (*ConflictReport, error) {
	// Use git merge-tree to simulate a merge
	cmd := exec.Command("git", "merge-tree", "--write-tree", base, treeA, treeB)
	cmd.Dir = c.repoPath

	output, err := cmd.CombinedOutput()
	outputStr := string(output)

	report := &ConflictReport{
		RawOutput: outputStr,
	}

	if err != nil {
		// Exit code 1 indicates conflicts
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			report.HasConflicts = true
			report.ConflictedFiles = c.parseConflicts(outputStr)
			return report, nil
		}
		// Other errors are actual failures
		return nil, err
	}

	// No conflicts - first line is the merged tree hash
	lines := strings.Split(strings.TrimSpace(outputStr), "\n")
	if len(lines) > 0 {
		report.MergedTreeHash = lines[0]
	}

	return report, nil
}

// parseConflicts extracts conflict information from git merge-tree output.
func (c *ConflictPredictor) parseConflicts(output string) []ConflictedFile {
	var conflicts []ConflictedFile

	lines := strings.Split(output, "\n")
	for _, line := range lines {
		// Look for conflict markers in the output
		// Format varies but typically includes file paths with conflict indicators
		if strings.Contains(line, "CONFLICT") || strings.Contains(line, "conflict") {
			// Extract the file path if present
			// Common format: "CONFLICT (content): Merge conflict in <path>"
			if idx := strings.Index(line, "Merge conflict in "); idx != -1 {
				path := strings.TrimSpace(line[idx+len("Merge conflict in "):])
				conflicts = append(conflicts, ConflictedFile{
					Path: path,
				})
			}
		}
	}

	return conflicts
}

// PredictConflictsSimple is a simplified version that just checks if two trees conflict.
func (c *ConflictPredictor) PredictConflictsSimple(base, treeA, treeB string) (bool, error) {
	report, err := c.PredictConflicts(base, treeA, treeB)
	if err != nil {
		return false, err
	}
	return report.HasConflicts, nil
}

// GetCommonAncestor finds the common ancestor (merge base) of two commits.
func (c *ConflictPredictor) GetCommonAncestor(commitA, commitB string) (string, error) {
	cmd := exec.Command("git", "merge-base", commitA, commitB)
	cmd.Dir = c.repoPath

	output, err := cmd.Output()
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(output)), nil
}

// DiffTrees shows the differences between two trees.
func (c *ConflictPredictor) DiffTrees(treeA, treeB string) ([]TreeDiff, error) {
	cmd := exec.Command("git", "diff-tree", "-r", "--name-status", treeA, treeB)
	cmd.Dir = c.repoPath

	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var diffs []TreeDiff
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		diffs = append(diffs, TreeDiff{
			Status: parts[0],
			Path:   parts[1],
		})
	}

	return diffs, nil
}

// TreeDiff represents a difference between two trees.
type TreeDiff struct {
	Status string // A=added, D=deleted, M=modified, R=renamed, C=copied
	Path   string
}

// OverlapsWith checks if the changes in treeA overlap with changes in treeB
// (both relative to the base tree).
func (c *ConflictPredictor) OverlapsWith(base, treeA, treeB string) ([]string, error) {
	// Get changes from base to treeA
	changesA, err := c.DiffTrees(base, treeA)
	if err != nil {
		return nil, err
	}

	// Get changes from base to treeB
	changesB, err := c.DiffTrees(base, treeB)
	if err != nil {
		return nil, err
	}

	// Build set of paths changed in A
	pathsA := make(map[string]bool)
	for _, d := range changesA {
		pathsA[d.Path] = true
	}

	// Find overlapping paths
	var overlapping []string
	for _, d := range changesB {
		if pathsA[d.Path] {
			overlapping = append(overlapping, d.Path)
		}
	}

	return overlapping, nil
}
