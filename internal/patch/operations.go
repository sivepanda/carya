package patch

import (
	"carya/internal/chunk"
	"fmt"
	"log"
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

func Apply(patch string) error {
	return runApply([]string{"--cached", "-"}, patch)
}

func CheckApply(patch string) error {
	return runApply([]string{"--check", "--cached", "-"}, patch)
}

func runApply(args []string, patch string) error {
	applyArgs := append([]string{"apply"}, args...)
	applyCmd := exec.Command("git", applyArgs...)
	applyCmd.Stdin = strings.NewReader(patch)

	if output, err := applyCmd.CombinedOutput(); err != nil {
		log.Printf("Failed to apply patch: %v, output: %s", err, output)
		return fmt.Errorf("failed to apply patch: %w\n%s", err, output)
	}

	return nil
}

func Commit(message string) (string, error) {
	commitCmd := exec.Command("git", "commit", "-m", message)
	output, err := commitCmd.CombinedOutput()
	if err != nil {
		log.Printf("Failed to create commit: %v, output: %s", err, output)
		return "", fmt.Errorf("failed to create commit: %w\n%s", err, output)
	}

	return string(output), nil
}
