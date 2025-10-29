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

		// Only run git pull if --no-pull is not set
		if !noPull {
			// Check if housekeeping.json changed during pull
			var err error
			housekeepingChanged, err = pullFromGit()
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

		executor := housekeeping.NewExecutor(config)
		if err := executor.ExecuteCategory("post-pull", autoApprove); err != nil {
			fmt.Fprintf(os.Stderr, "Error executing post-pull commands: %v\n", err)
			os.Exit(1)
		}
	},
}

// pullFromGit executes git pull and returns whether housekeeping.json was changed
func pullFromGit() (bool, error) {
	// Get the path to housekeeping.json relative to git root
	wd, err := os.Getwd()
	if err != nil {
		return false, fmt.Errorf("failed to get working directory: %w", err)
	}

	caryaDir := filepath.Join(wd, ".carya")
	housekeepingPath := filepath.Join(caryaDir, "housekeeping.json")
	relPath, err := filepath.Rel(wd, housekeepingPath)
	if err != nil {
		return false, fmt.Errorf("failed to get relative path: %w", err)
	}

	// Get the hash of housekeeping.json before pull
	beforeHash, _ := getFileHash(relPath)

	// Execute git pull
	fmt.Println("Pulling from git...")
	pullCmd := exec.Command("git", "pull")
	pullCmd.Stdout = os.Stdout
	pullCmd.Stderr = os.Stderr
	pullCmd.Dir = wd

	if err := pullCmd.Run(); err != nil {
		return false, fmt.Errorf("git pull failed: %w", err)
	}

	// Get the hash of housekeeping.json after pull
	afterHash, _ := getFileHash(relPath)

	// Check if the file changed
	changed := beforeHash != "" && afterHash != "" && beforeHash != afterHash

	return changed, nil
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

func init() {
	pullCmd.Flags().Bool("auto", false, "Run post-pull commands without confirmation")
	pullCmd.Flags().Bool("no-pull", false, "Skip git pull and only run post-pull commands")
	rootCmd.AddCommand(pullCmd)
}
