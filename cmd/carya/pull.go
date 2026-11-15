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
			fmt.Println("The post-pull commands below reflect the new configuration.")
		}

		// Load and execute post-pull commands
		config, err := mycelia.LoadConfig()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading housekeeping config: %v\n", err)
			os.Exit(1)
		}

		// Check for auto-approve in config if flag not set
		if !autoApprove {
			autoApprove = config.IsAutoApprove("post-pull")
		}

		executor := mycelia.NewExecutor(config)
		if err := executor.ExecuteCategoryWithChangedFiles("post-pull", changedFiles, autoApprove); err != nil {
			fmt.Fprintf(os.Stderr, "Error executing post-pull commands: %v\n", err)
			os.Exit(1)
		}
	},
}

// pullFromGit executes git pull and returns whether carya.json was changed and the list of changed files
func pullFromGit() (bool, []string, error) {
	configPath, err := mycelia.GetConfigPath()
	if err != nil {
		log.Printf("Failed to get config path: %v", err)
		return false, nil, fmt.Errorf("failed to get config path: %w", err)
	}

	return git.TrackConfigChange(configPath, func(wd string) error {
		fmt.Println("Pulling from git...")
		pullCmd := exec.Command("git", "pull")
		pullCmd.Stdout = os.Stdout
		pullCmd.Stderr = os.Stderr
		pullCmd.Dir = wd

		if err := pullCmd.Run(); err != nil {
			log.Printf("Git pull failed: %v", err)
			return fmt.Errorf("git pull failed: %w", err)
		}
		return nil
	})
}

func init() {
	pullCmd.Flags().BoolP("auto", "y", false, "Run post-pull commands without confirmation")
	pullCmd.Flags().Bool("no-pull", false, "Skip git pull and only run post-pull commands")
	rootCmd.AddCommand(pullCmd)
}
