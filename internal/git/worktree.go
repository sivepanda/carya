package git

import (
	"fmt"
	"os/exec"
	"strings"
)

// WorkingTreeChanges lists the paths whose working-tree state differs from
// HEAD: modified, added and deleted tracked files, plus untracked files that
// are not ignored. In a repository without commits, every tracked and
// untracked file is listed.
func WorkingTreeChanges(repoRoot string) ([]string, error) {
	tracked := []string{"diff", "--name-only", "-z", "--no-renames", "HEAD", "--"}
	verify := exec.Command("git", "rev-parse", "--verify", "-q", "HEAD")
	verify.Dir = repoRoot
	if verify.Run() != nil {
		tracked = []string{"ls-files", "-z", "--cached"}
	}
	untracked := []string{"ls-files", "-z", "--others", "--exclude-standard"}

	seen := make(map[string]bool)
	var paths []string
	for _, args := range [][]string{tracked, untracked} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repoRoot
		output, err := cmd.Output()
		if err != nil {
			return nil, fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
		}
		for p := range strings.SplitSeq(string(output), "\x00") {
			if p != "" && !seen[p] {
				seen[p] = true
				paths = append(paths, p)
			}
		}
	}
	return paths, nil
}
