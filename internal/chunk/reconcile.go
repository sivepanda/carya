package chunk

import (
	"os/exec"
	"strings"
)

type ReconcileDecision string

const (
	DecisionKeep  ReconcileDecision = "keep"
	DecisionPrune ReconcileDecision = "prune"
)

// DecideRetention determines whether a chunk should be kept based on the current git state.
// Chunks are pruned when they appear already applied/committed or when they no longer match.
func DecideRetention(repoPath string, c Chunk) (ReconcileDecision, string) {
	patch := strings.TrimSpace(c.Diff)
	if patch == "" {
		return DecisionPrune, "empty diff"
	}

	if strings.HasPrefix(patch, "Binary file ") {
		modified, err := isPathModified(repoPath, c.FilePath)
		if err != nil {
			return DecisionPrune, "binary diff check failed"
		}
		if modified {
			return DecisionKeep, "binary path still modified"
		}
		return DecisionPrune, "binary path matches HEAD"
	}

	if appliesReverse(repoPath, c.Diff) {
		return DecisionPrune, "already applied/committed"
	}

	if appliesForward(repoPath, c.Diff) {
		return DecisionKeep, "still applicable"
	}

	return DecisionPrune, "no longer matches"
}

func appliesForward(repoPath, patch string) bool {
	cmd := exec.Command("git", "apply", "--check", "--cached", "-")
	cmd.Dir = repoPath
	cmd.Stdin = strings.NewReader(patch)
	return cmd.Run() == nil
}

func appliesReverse(repoPath, patch string) bool {
	cmd := exec.Command("git", "apply", "--check", "--cached", "--reverse", "-")
	cmd.Dir = repoPath
	cmd.Stdin = strings.NewReader(patch)
	return cmd.Run() == nil
}

func isPathModified(repoPath, path string) (bool, error) {
	cmd := exec.Command("git", "diff", "--quiet", "HEAD", "--", path)
	cmd.Dir = repoPath
	err := cmd.Run()
	if err == nil {
		return false, nil
	}
	if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
		return true, nil
	}
	return false, err
}
