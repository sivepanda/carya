package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"carya/internal/git"
	"carya/internal/repository"

	"github.com/spf13/cobra"
)

var forceJump bool

var jumpCmd = &cobra.Command{
	Use:   "jump <user>",
	Short: "Jump into another user's working state",
	Long: `Checkout another team member's working state into your working directory.

This will modify files in your working directory to match the target user's state.
Make sure you have committed or stashed any changes you want to keep.

Use --force to skip the confirmation prompt.`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		targetUser := args[0]

		repo, err := repository.New()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		if !repo.Exists() {
			fmt.Fprintf(os.Stderr, "Error: Not a Carya repository. Run 'carya init' first.\n")
			os.Exit(1)
		}

		refManager := git.NewRefManager(repo.RootPath())

		// Get target user's tree hash
		treeHash, err := refManager.GetUserTreeRef(targetUser)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: User '%s' hasn't published their state.\n", targetUser)
			fmt.Fprintf(os.Stderr, "Use 'carya team --fetch' to get the latest refs from origin.\n")
			os.Exit(1)
		}

		// Show what will change
		fmt.Printf("Jumping to %s's working state (tree: %s)\n", targetUser, treeHash[:12])

		// Get current HEAD tree for comparison
		headTree, err := refManager.GetHEADTreeHash()
		if err == nil {
			predictor := git.NewConflictPredictor(repo.RootPath())
			diffs, err := predictor.DiffTrees(headTree, treeHash)
			if err == nil && len(diffs) > 0 {
				fmt.Println("\nFiles that will be modified:")
				for _, d := range diffs {
					status := "?"
					switch d.Status {
					case "A":
						status = "add"
					case "D":
						status = "del"
					case "M":
						status = "mod"
					}
					fmt.Printf("  [%s] %s\n", status, d.Path)
				}
				fmt.Println()
			}
		}

		// Confirm unless --force
		if !forceJump {
			fmt.Print("This will modify your working directory. Continue? [y/N] ")
			reader := bufio.NewReader(os.Stdin)
			response, _ := reader.ReadString('\n')
			response = strings.TrimSpace(strings.ToLower(response))
			if response != "y" && response != "yes" {
				fmt.Println("Aborted.")
				os.Exit(0)
			}
		}

		// Checkout the tree
		// Use git checkout with the ref to get the files
		refPath := fmt.Sprintf("refs/carya/users/%s/tree", targetUser)
		gitCmd := exec.Command("git", "checkout", refPath, "--", ".")
		gitCmd.Dir = repo.RootPath()
		gitCmd.Stdout = os.Stdout
		gitCmd.Stderr = os.Stderr

		if err := gitCmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "Error checking out tree: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("\nSuccessfully jumped to %s's working state.\n", targetUser)
		fmt.Println("Your working directory now reflects their changes.")
		fmt.Println("\nTo return to your previous state:")
		fmt.Println("  git checkout HEAD -- .")
	},
}

func init() {
	jumpCmd.Flags().BoolVarP(&forceJump, "force", "f", false, "Skip confirmation prompt")
	rootCmd.AddCommand(jumpCmd)
}
