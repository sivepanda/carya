package git

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// TrackConfigChange runs an action and reports whether configPath changed,
// plus the files changed between the pre/post action HEAD commits.
func TrackConfigChange(configPath string, action func(repoPath string) error) (bool, []string, error) {
	repoPath, err := os.Getwd()
	if err != nil {
		log.Printf("Failed to get working directory: %v", err)
		return false, nil, fmt.Errorf("failed to get working directory: %w", err)
	}

	relPath, err := filepath.Rel(repoPath, configPath)
	if err != nil {
		log.Printf("Failed to get relative path: %v", err)
		return false, nil, fmt.Errorf("failed to get relative path: %w", err)
	}

	beforeHash, _ := fileHash(relPath)

	beforeCommit, err := headCommit()
	if err != nil {
		log.Printf("Failed to get HEAD commit: %v", err)
		return false, nil, fmt.Errorf("failed to get HEAD commit: %w", err)
	}

	if err := action(repoPath); err != nil {
		return false, nil, err
	}

	afterHash, _ := fileHash(relPath)
	configChanged := beforeHash != "" && afterHash != "" && beforeHash != afterHash

	changedFiles, err := changedFilesSince(beforeCommit)
	if err != nil {
		changedFiles = []string{}
	}

	return configChanged, changedFiles, nil
}

func fileHash(path string) (string, error) {
	cmd := exec.Command("git", "hash-object", path)
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(output)), nil
}

func headCommit() (string, error) {
	cmd := exec.Command("git", "rev-parse", "HEAD")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(output)), nil
}

func changedFilesSince(fromCommit string) ([]string, error) {
	currentCommit, err := headCommit()
	if err != nil {
		return nil, err
	}

	if fromCommit == currentCommit {
		return []string{}, nil
	}

	cmd := exec.Command("git", "diff", "--name-only", fromCommit, currentCommit)
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	files := strings.Split(strings.TrimSpace(string(output)), "\n")
	result := make([]string, 0, len(files))
	for _, file := range files {
		if file != "" {
			result = append(result, file)
		}
	}

	return result, nil
}
