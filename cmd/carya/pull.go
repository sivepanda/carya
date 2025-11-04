package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"carya/internal/housekeeping"

	"github.com/spf13/cobra"
)

var pullCmd = &cobra.Command{
	Use:   "pull",
	Short: "Pull from git and run post-pull housekeeping tasks",
	Long:  `Execute git pull, detect changes in housekeeping config, and run configured post-pull commands.`,
	Run: func(cmd *cobra.Command, args []string) {
		autoApprove, _ := cmd.Flags().GetBool("auto")
		noPull, _ := cmd.Flags().GetBool("no-pull")

		var housekeepingChanged bool
		var changedFiles []string

		// Only run git pull if --no-pull is not set
		if !noPull {
			// Check if housekeeping.json changed during pull
			var err error
			housekeepingChanged, changedFiles, err = pullFromGit()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error pulling from git: %v\n", err)
				os.Exit(1)
			}
		}

		// Notify user if housekeeping config changed
		if housekeepingChanged {
			fmt.Println("\n⚠️  Housekeeping configuration was updated during pull")
			fmt.Println("The post-pull commands below reflect the new configuration.\n")
		}

		// Load and execute post-pull commands
		config, err := housekeeping.LoadConfig()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading housekeeping config: %v\n", err)
			os.Exit(1)
		}

		// Check for auto-approve in config if flag not set
		if !autoApprove {
			autoApprove = config.IsAutoApprove("post-pull")
		}

		executor := housekeeping.NewExecutor(config)
		if err := executor.ExecuteCategoryWithChangedFiles("post-pull", changedFiles, autoApprove); err != nil {
			fmt.Fprintf(os.Stderr, "Error executing post-pull commands: %v\n", err)
			os.Exit(1)
		}
	},
}

// pullFromGit executes git pull and returns whether housekeeping.json was changed and the list of changed files
func pullFromGit() (bool, []string, error) {
	// Get the path to housekeeping.json relative to git root
	wd, err := os.Getwd()
	if err != nil {
		return false, nil, fmt.Errorf("failed to get working directory: %w", err)
	}

	caryaDir := filepath.Join(wd, ".carya")
	housekeepingPath := filepath.Join(caryaDir, "housekeeping.json")
	relPath, err := filepath.Rel(wd, housekeepingPath)
	if err != nil {
		return false, nil, fmt.Errorf("failed to get relative path: %w", err)
	}

	// Get the hash of housekeeping.json before pull
	beforeHash, _ := getFileHash(relPath)

	// Get the current HEAD commit before pull
	beforeCommit, err := getHeadCommit()
	if err != nil {
		return false, nil, fmt.Errorf("failed to get HEAD commit: %w", err)
	}

	// Execute git pull
	fmt.Println("Pulling from git...")
	pullCmd := exec.Command("git", "pull")
	pullCmd.Stdout = os.Stdout
	pullCmd.Stderr = os.Stderr
	pullCmd.Dir = wd

	if err := pullCmd.Run(); err != nil {
		return false, nil, fmt.Errorf("git pull failed: %w", err)
	}

	// Get the hash of housekeeping.json after pull
	afterHash, _ := getFileHash(relPath)

	// Check if the file changed
	housekeepingChanged := beforeHash != "" && afterHash != "" && beforeHash != afterHash

	// Get the list of changed files
	changedFiles, err := getChangedFiles(beforeCommit)
	if err != nil {
		// Don't fail if we can't get changed files, just return empty list
		changedFiles = []string{}
	}

	return housekeepingChanged, changedFiles, nil
}

// getFileHash returns the git hash of a file
func getFileHash(filepath string) (string, error) {
	cmd := exec.Command("git", "hash-object", filepath)
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(output)), nil
}

// getHeadCommit returns the current HEAD commit hash
func getHeadCommit() (string, error) {
	cmd := exec.Command("git", "rev-parse", "HEAD")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(output)), nil
}

// getChangedFiles returns the list of files changed between a commit and HEAD
func getChangedFiles(fromCommit string) ([]string, error) {
	currentCommit, err := getHeadCommit()
	if err != nil {
		return nil, err
	}

	// If commits are the same, no changes occurred
	if fromCommit == currentCommit {
		return []string{}, nil
	}

	// Get the list of changed files using git diff
	cmd := exec.Command("git", "diff", "--name-only", fromCommit, currentCommit)
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	files := strings.Split(strings.TrimSpace(string(output)), "\n")
	// Filter out empty strings
	var result []string
	for _, file := range files {
		if file != "" {
			result = append(result, file)
		}
	}

	return result, nil
}

func init() {
	pullCmd.Flags().BoolP("auto", "y", false, "Run post-pull commands without confirmation")
	pullCmd.Flags().Bool("no-pull", false, "Skip git pull and only run post-pull commands")
	rootCmd.AddCommand(pullCmd)
}
