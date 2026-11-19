package git

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// WorkingStateTree writes the working tree as it stands as a tree object:
// tracked changes and deletions plus untracked files that are not ignored.
// It works on a throwaway copy of the index, so the repository's index and
// staging area are untouched, and it needs no daemon or shadow index. That
// makes it suitable for agents, whose edits also arrive through shell
// commands a file watcher would only see later.
//
// Nested repositories (linked worktrees under the checkout, such as
// .claude/worktrees/*) and .carya are left out rather than recorded as
// embedded-repository entries.
func WorkingStateTree(repoRoot string) (string, error) {
	tmp, err := os.CreateTemp("", "carya-state-index-*")
	if err != nil {
		return "", fmt.Errorf("create temp index: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	seeded, err := copyRealIndex(repoRoot, tmp)
	_ = tmp.Close()
	if err != nil {
		return "", err
	}

	env := append(os.Environ(), "GIT_INDEX_FILE="+tmpPath)
	git := func(stdin string, args ...string) (string, error) {
		cmd := exec.Command("git", args...)
		cmd.Dir = repoRoot
		cmd.Env = env
		if stdin != "" {
			cmd.Stdin = strings.NewReader(stdin)
		}
		out, err := cmd.Output()
		if err != nil {
			var stderr string
			if exitErr, ok := err.(*exec.ExitError); ok {
				stderr = strings.TrimSpace(string(exitErr.Stderr))
			}
			return "", fmt.Errorf("git %s: %w %s", strings.Join(args, " "), err, stderr)
		}
		return string(out), nil
	}

	if !seeded {
		// No index yet: start from HEAD if there is one, else from empty.
		// An empty file is not a valid index, so let git create it.
		_ = os.Remove(tmpPath)
		if _, err := git("", "rev-parse", "--verify", "--quiet", "HEAD^{commit}"); err == nil {
			if _, err := git("", "read-tree", "HEAD"); err != nil {
				return "", err
			}
		}
	}

	if _, err := git("", "add", "--update"); err != nil {
		return "", err
	}

	others, err := git("", "ls-files", "-z", "--others", "--exclude-standard")
	if err != nil {
		return "", err
	}
	var add []string
	for _, p := range strings.Split(others, "\x00") {
		// ls-files reports a nested repository as its directory with a
		// trailing slash instead of descending into it.
		if p == "" || strings.HasSuffix(p, "/") || p == ".carya" || strings.HasPrefix(p, ".carya/") {
			continue
		}
		add = append(add, p)
	}
	if len(add) > 0 {
		if _, err := git(strings.Join(add, "\x00")+"\x00", "update-index", "--add", "-z", "--stdin"); err != nil {
			return "", err
		}
	}

	tree, err := git("", "write-tree")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(tree), nil
}

// copyRealIndex copies the checkout's index into dst so stat information
// lets git skip rehashing unchanged files. It reports false when the
// checkout has no index yet.
func copyRealIndex(repoRoot string, dst *os.File) (bool, error) {
	cmd := exec.Command("git", "rev-parse", "--git-path", "index")
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		return false, fmt.Errorf("locate index: %w", err)
	}
	indexPath := strings.TrimSpace(string(out))
	if !filepath.IsAbs(indexPath) {
		indexPath = filepath.Join(repoRoot, indexPath)
	}

	src, err := os.Open(indexPath)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("open index: %w", err)
	}
	defer src.Close()
	if _, err := io.Copy(dst, src); err != nil {
		return false, fmt.Errorf("copy index: %w", err)
	}
	return true, nil
}
