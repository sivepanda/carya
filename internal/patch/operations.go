package patch

import (
	"carya/internal/chunk"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

type Result struct {
	Patch    string
	Warnings []string
}

// CreateFromChunks joins the selected chunks' diffs into one patch, in the
// order the chunks were recorded: a file's later chunks are diffed on top of
// its earlier ones, and git apply takes a file's diffs in sequence.
func CreateFromChunks(chunks []chunk.Chunk, selectedIndices map[int]bool) Result {
	var patches []string
	var warnings []string

	var order []int
	for i := range chunks {
		if selectedIndices[i] {
			order = append(order, i)
		}
	}
	sort.SliceStable(order, func(a, b int) bool {
		return chunk.RecordedBefore(chunks[order[a]], chunks[order[b]])
	})

	for _, i := range order {
		c := chunks[i]
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

// runApply applies patch with git apply exactly as it is: apply.whitespace=fix
// would otherwise change what is committed, and =error refuse it.
func runApply(repoPath string, env, args []string, patch string) error {
	applyArgs := append([]string{"apply", "--whitespace=nowarn"}, args...)
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
	if err := CheckNoOperation(repoPath); err != nil {
		return "", err
	}
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

// CheckNoOperation refuses to commit while git is in the middle of a merge,
// cherry-pick or revert: the commit would become that operation's, recording
// its other parent or taking its author, with only some of its changes.
func CheckNoOperation(repoPath string) error {
	cmd := exec.Command("git", "rev-parse", "--git-path", "MERGE_HEAD", "--git-path", "CHERRY_PICK_HEAD", "--git-path", "REVERT_HEAD")
	cmd.Dir = repoPath
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("find git's operation state: %w", err)
	}
	paths := strings.Split(strings.TrimSpace(string(out)), "\n")
	for i, op := range []string{"merge", "cherry-pick", "revert"} {
		if i >= len(paths) {
			break
		}
		p := paths[i]
		if !filepath.IsAbs(p) {
			p = filepath.Join(repoPath, p)
		}
		if _, err := os.Stat(p); err == nil {
			return fmt.Errorf("a %s is in progress; finish or abort it before committing", op)
		}
	}
	return nil
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
