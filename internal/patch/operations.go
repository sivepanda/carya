package patch

import (
	"carya/internal/chunk"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
)

type Result struct {
	Patch    string
	Warnings []string
}

func CreateFromChunks(chunks []chunk.Chunk, selectedIndices map[int]bool) Result {
	var patches []string
	var warnings []string

	for i, c := range chunks {
		if selected, ok := selectedIndices[i]; ok && selected {
			cleanDiff, diffWarnings := CleanupDiffForGit(c)
			if len(diffWarnings) > 0 {
				for _, w := range diffWarnings {
					warnings = append(warnings, fmt.Sprintf("%s: %s", c.FilePath, w))
				}
			}
			if cleanDiff != "" {
				patches = append(patches, cleanDiff)
			}
		}
	}

	return Result{
		Patch:    strings.Join(patches, ""),
		Warnings: warnings,
	}
}

func CleanupDiffForGit(c chunk.Chunk) (string, []string) {
	diff := c.Diff
	var warnings []string

	if strings.HasPrefix(diff, "Binary file ") {
		return "", nil
	}

	if !strings.HasSuffix(diff, "\n") {
		diff = diff + "\n"
	}

	if strings.Contains(diff, "\x00") {
		warning := "Diff contains null bytes (file may be binary)"
		warnings = append(warnings, warning)
		return "", warnings
	}

	diff = strings.ReplaceAll(diff, "\r\n", "\n")

	return diff, warnings
}

func CheckApply(repoPath, patch string) error {
	return withHeadIndex(repoPath, func(env []string) error {
		return runApply(repoPath, env, []string{"--check", "--cached", "-"}, patch)
	})
}

func runApply(repoPath string, env, args []string, patch string) error {
	applyArgs := append([]string{"apply"}, args...)
	applyCmd := exec.Command("git", applyArgs...)
	applyCmd.Dir = repoPath
	applyCmd.Env = env
	applyCmd.Stdin = strings.NewReader(patch)

	if output, err := applyCmd.CombinedOutput(); err != nil {
		log.Printf("Failed to apply patch: %v, output: %s", err, output)
		return fmt.Errorf("failed to apply patch: %w\n%s", err, output)
	}

	return nil
}

func ApplyAndCommit(repoPath, patch, message string) (string, error) {
	var result string
	err := withHeadIndex(repoPath, func(env []string) error {
		if err := runApply(repoPath, env, []string{"--cached", "-"}, patch); err != nil {
			return err
		}
		commitCmd := exec.Command("git", "commit", "-m", message)
		commitCmd.Dir = repoPath
		commitCmd.Env = env
		output, err := commitCmd.CombinedOutput()
		if err != nil {
			log.Printf("Failed to create commit: %v, output: %s", err, output)
			return fmt.Errorf("failed to create commit: %w\n%s", err, output)
		}
		result = string(output)
		return nil
	})
	if err != nil {
		return result, err
	}
	if err := syncRealIndex(repoPath); err != nil {
		return result, fmt.Errorf("commit created but failed to refresh index: %w", err)
	}
	return result, nil
}

// syncRealIndex updates the repository's real index to match the new HEAD for
// the paths touched by the commit just created via a temporary index. Without
// this, git status would show the committed changes as staged reversions.
func syncRealIndex(repoPath string) error {
	listCmd := exec.Command("git", "diff-tree", "--no-commit-id", "--name-only", "--root", "-r", "-z", "HEAD")
	listCmd.Dir = repoPath
	output, err := listCmd.Output()
	if err != nil {
		return fmt.Errorf("list committed paths: %w", err)
	}

	var paths []string
	for p := range strings.SplitSeq(string(output), "\x00") {
		if p != "" {
			paths = append(paths, p)
		}
	}
	if len(paths) == 0 {
		return nil
	}

	for _, path := range paths {
		// A path that differed from the commit parent was already staged by the
		// user. Leave it alone rather than replacing their staged content.
		if headHasParent(repoPath) {
			parentDiff := exec.Command("git", "diff", "--cached", "--quiet", "HEAD^", "--", path)
			parentDiff.Dir = repoPath
			if err := parentDiff.Run(); err != nil {
				continue
			}
		}

		resetCmd := exec.Command("git", "reset", "-q", "HEAD", "--", path)
		resetCmd.Dir = repoPath
		if out, err := resetCmd.CombinedOutput(); err != nil {
			return fmt.Errorf("reset index for committed path %q: %w\n%s", path, err, out)
		}
	}
	return nil
}

func headHasParent(repoPath string) bool {
	cmd := exec.Command("git", "rev-parse", "--verify", "--quiet", "HEAD^")
	cmd.Dir = repoPath
	return cmd.Run() == nil
}

func withHeadIndex(repoPath string, fn func([]string) error) error {
	indexFile, err := os.CreateTemp("", "carya-compose-index-*")
	if err != nil {
		return fmt.Errorf("create temporary index: %w", err)
	}
	indexPath := indexFile.Name()
	if err := indexFile.Close(); err != nil {
		_ = os.Remove(indexPath)
		return fmt.Errorf("close temporary index: %w", err)
	}
	defer os.Remove(indexPath)

	env := append(os.Environ(), "GIT_INDEX_FILE="+indexPath)
	readTreeArgs := []string{"read-tree", "HEAD"}
	if !headExists(repoPath) {
		// Unborn branch (no commits yet): seed an empty index instead.
		readTreeArgs = []string{"read-tree", "--empty"}
	}
	readTree := exec.Command("git", readTreeArgs...)
	readTree.Dir = repoPath
	readTree.Env = env
	if output, err := readTree.CombinedOutput(); err != nil {
		return fmt.Errorf("seed temporary index: %w\n%s", err, output)
	}
	return fn(env)
}

func headExists(repoPath string) bool {
	cmd := exec.Command("git", "rev-parse", "--verify", "--quiet", "HEAD")
	cmd.Dir = repoPath
	return cmd.Run() == nil
}
