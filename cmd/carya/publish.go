package main

import (
	"fmt"
	"log"
	"os"

	"carya/internal/git"
	"carya/internal/identity"

	"github.com/spf13/cobra"
)

var localOnlyFlag bool

var publishCmd = &cobra.Command{
	Use:   "publish",
	Short: "Publish current working state to team",
	Long: `Publish your current working tree state as a git ref that team members can see.

This creates or updates refs/carya/users/<your-user-id>/tree with your current
working state, and pushes it to the remote repository so team members can see
it. Use --local-only to skip the push and only update the local ref.`,
	Run: func(cmd *cobra.Command, args []string) {
		repo := mustInitializedRepo()

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
		refPath := git.UserTreeRefPath(userID)
		fmt.Printf("  Ref: %s\n", refPath)
		log.Printf("  Ref: %s\n", refPath)

		// Push unless explicitly disabled
		if !localOnlyFlag {
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
	publishCmd.Flags().BoolVar(&localOnlyFlag, "local-only", false, "Only update the local ref, don't push to origin")
	rootCmd.AddCommand(publishCmd)
}
