package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"carya/internal/housekeeping"

	"github.com/spf13/cobra"
)

var checkoutCmd = &cobra.Command{
	Use:   "checkout [branch]",
	Short: "Checkout a git branch and run post-checkout housekeeping tasks",
	Long:  `Execute git checkout, detect changes, and run configured post-checkout commands.`,
	Args:  cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		autoApprove, _ := cmd.Flags().GetBool("auto")
		noCheckout, _ := cmd.Flags().GetBool("no-checkout")
		branch := args[0]

		var housekeepingChanged bool
		var changedFiles []string

		// Only run git checkout if --no-checkout is not set
		if !noCheckout {
			var err error
			housekeepingChanged, changedFiles, err = checkoutBranch(branch)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error checking out branch: %v\n", err)
				os.Exit(1)
			}
		}

		// Notify user if housekeeping config changed
		if housekeepingChanged {
			fmt.Println("\n⚠️  Housekeeping configuration was updated during checkout")
			fmt.Println("The post-checkout commands below reflect the new configuration.\n")
		}

		// Load and execute post-checkout commands
		config, err := housekeeping.LoadConfig()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading housekeeping config: %v\n", err)
			os.Exit(1)
		}

		// Check for auto-approve in config if flag not set
		if !autoApprove {
			autoApprove = config.IsAutoApprove("post-checkout")
		}

		executor := housekeeping.NewExecutor(config)
		if err := executor.ExecuteCategoryWithChangedFiles("post-checkout", changedFiles, autoApprove); err != nil {
			fmt.Fprintf(os.Stderr, "Error executing post-checkout commands: %v\n", err)
			os.Exit(1)
		}
	},
}

// checkoutBranch executes git checkout and returns whether housekeeping.json was changed and the list of changed files
func checkoutBranch(branch string) (bool, []string, error) {
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

	// Get the hash of housekeeping.json before checkout
	beforeHash, _ := getFileHash(relPath)

	// Get the current HEAD commit before checkout
	beforeCommit, err := getHeadCommit()
	if err != nil {
		return false, nil, fmt.Errorf("failed to get HEAD commit: %w", err)
	}

	// Execute git checkout
	fmt.Printf("Checking out branch '%s'...\n", branch)
	checkoutCmd := exec.Command("git", "checkout", branch)
	checkoutCmd.Stdout = os.Stdout
	checkoutCmd.Stderr = os.Stderr
	checkoutCmd.Dir = wd

	if err := checkoutCmd.Run(); err != nil {
		return false, nil, fmt.Errorf("git checkout failed: %w", err)
	}

	// Get the hash of housekeeping.json after checkout
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

func init() {
	checkoutCmd.Flags().BoolP("auto", "y", false, "Run post-checkout commands without confirmation")
	checkoutCmd.Flags().Bool("no-checkout", false, "Skip git checkout and only run post-checkout commands")
	rootCmd.AddCommand(checkoutCmd)
}
