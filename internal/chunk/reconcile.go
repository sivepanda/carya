package chunk

import (
	"os"
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

	if appliesReverseToRef(repoPath, c.Diff, "HEAD") {
		return DecisionPrune, "already applied/committed"
	}

	if appliesReverseUpstream(repoPath, c.Diff) {
		return DecisionPrune, "already in upstream"
	}

	if appliesForward(repoPath, c.Diff) {
		return DecisionKeep, "still applicable"
	}

	return DecisionPrune, "no longer matches"
}

func appliesForward(repoPath, patch string) bool {
	return appliesToRef(repoPath, patch, "HEAD", false)
}

func appliesReverseUpstream(repoPath, patch string) bool {
	upstreamRef := currentUpstreamRef(repoPath)
	if upstreamRef == "" {
		return false
	}

	return appliesReverseToRef(repoPath, patch, upstreamRef)
}

func currentUpstreamRef(repoPath string) string {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}")
	cmd.Dir = repoPath
	out, err := cmd.Output()
	if err != nil {
		return ""
	}

	return strings.TrimSpace(string(out))
}

func appliesReverseToRef(repoPath, patch, ref string) bool {
	return appliesToRef(repoPath, patch, ref, true)
}

func appliesToRef(repoPath, patch, ref string, reverse bool) bool {
	indexFile, err := os.CreateTemp("", "carya-upstream-index-*")
	if err != nil {
		return false
	}
	indexPath := indexFile.Name()
	_ = indexFile.Close()
	defer os.Remove(indexPath)

	readTreeCmd := exec.Command("git", "read-tree", ref)
	readTreeCmd.Dir = repoPath
	readTreeCmd.Env = append(os.Environ(), "GIT_INDEX_FILE="+indexPath)
	if err := readTreeCmd.Run(); err != nil {
		return false
	}

	applyArgs := []string{"apply", "--check", "--cached"}
	if reverse {
		applyArgs = append(applyArgs, "--reverse")
	}
	applyArgs = append(applyArgs, "-")
	applyCmd := exec.Command("git", applyArgs...)
	applyCmd.Dir = repoPath
	applyCmd.Env = append(os.Environ(), "GIT_INDEX_FILE="+indexPath)
	applyCmd.Stdin = strings.NewReader(patch)
	return applyCmd.Run() == nil
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
