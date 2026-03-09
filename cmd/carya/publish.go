package main

import (
	"fmt"
	"log"
	"os"

	"carya/internal/git"
	"carya/internal/identity"
	"carya/internal/repository"

	"github.com/spf13/cobra"
)

var pushFlag bool

var publishCmd = &cobra.Command{
	Use:   "publish",
	Short: "Publish current working state to team",
	Long: `Publish your current working tree state as a git ref that team members can see.

This creates or updates refs/carya/users/<your-user-id>/tree with your current
working state. Use --push to also push the ref to the remote repository.`,
	Run: func(cmd *cobra.Command, args []string) {
		repo, err := repository.New()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		if !repo.Exists() {
			fmt.Fprintf(os.Stderr, "Error: Not a Carya repository. Run 'carya init' first.\n")
			os.Exit(1)
		}

		// Get user identity
		userIdentity := identity.NewUserIdentity(repo.CaryaPath())
		userID, err := userIdentity.Get()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		// Initialize shadow repo
		shadow := git.NewShadowRepo(repo.CaryaPath(), repo.RootPath())

		// Write tree from shadow index
		treeHash, err := shadow.WriteTree()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error writing tree: %v\n", err)
			os.Exit(1)
		}

		// Update ref
		refManager := git.NewRefManager(repo.RootPath())
		if err := refManager.UpdateUserTreeRef(userID, treeHash); err != nil {
			fmt.Fprintf(os.Stderr, "Error updating ref: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("Published working state as %s\n", userID)
		log.Printf("Published working state as %s\n", userID)
		fmt.Printf("  Tree hash: %s\n", treeHash[:12])
		log.Printf("  Tree hash: %s\n", treeHash[:12])
		fmt.Printf("  Ref: refs/carya/users/%s/tree\n", userID)
		log.Printf("  Ref: refs/carya/users/%s/tree\n", userID)

		// Push if requested
		if pushFlag {
			fmt.Println("Pushing to origin...")
			if err := refManager.PushUserRef("origin", userID); err != nil {
				fmt.Fprintf(os.Stderr, "Error pushing ref: %v\n", err)
				os.Exit(1)
			}
			fmt.Println("Pushed to origin")
		}
	},
}

func init() {
	publishCmd.Flags().BoolVar(&pushFlag, "push", false, "Push the ref to origin")
	rootCmd.AddCommand(publishCmd)
}
