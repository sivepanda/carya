package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"

	"carya/internal/git"

	"github.com/sivepanda/mycelia"

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
			fmt.Println("The post-checkout commands below reflect the new configuration.")
		}

		// Load and execute post-checkout commands
		config, err := mycelia.LoadConfig()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading housekeeping config: %v\n", err)
			os.Exit(1)
		}

		// Check for auto-approve in config if flag not set
		if !autoApprove {
			autoApprove = config.IsAutoApprove("post-checkout")
		}

		executor := mycelia.NewExecutor(config)
		if err := executor.ExecuteCategoryWithChangedFiles("post-checkout", changedFiles, autoApprove); err != nil {
			fmt.Fprintf(os.Stderr, "Error executing post-checkout commands: %v\n", err)
			os.Exit(1)
		}
	},
}

// checkoutBranch executes git checkout and returns whether carya.json was changed and the list of changed files
func checkoutBranch(branch string) (bool, []string, error) {
	configPath, err := mycelia.GetConfigPath()
	if err != nil {
		log.Printf("Failed to get config path: %v", err)
		return false, nil, fmt.Errorf("failed to get config path: %w", err)
	}

	return git.TrackConfigChange(configPath, func(wd string) error {
		fmt.Printf("Checking out branch '%s'...\n", branch)
		checkoutCmd := exec.Command("git", "checkout", branch)
		checkoutCmd.Stdout = os.Stdout
		checkoutCmd.Stderr = os.Stderr
		checkoutCmd.Dir = wd

		if err := checkoutCmd.Run(); err != nil {
			log.Printf("Git checkout failed: %v", err)
			return fmt.Errorf("git checkout failed: %w", err)
		}
		return nil
	})
}

func init() {
	checkoutCmd.Flags().BoolP("auto", "y", false, "Run post-checkout commands without confirmation")
	checkoutCmd.Flags().Bool("no-checkout", false, "Skip git checkout and only run post-checkout commands")
	rootCmd.AddCommand(checkoutCmd)
}
