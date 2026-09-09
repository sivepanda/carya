package main

import (
	"fmt"
	"os"

	"carya/internal/git"
	"carya/internal/identity"

	"github.com/spf13/cobra"
)

var fetchFlag bool

var teamCmd = &cobra.Command{
	Use:   "team",
	Short: "View team members' working states",
	Long: `List all team members who have published their working states.

Use --fetch to first fetch the latest refs from origin.`,
	Run: func(cmd *cobra.Command, args []string) {
		repo := mustInitializedRepo()

		refManager := git.NewRefManager(repo.RootPath())

		// Fetch if requested
		if fetchFlag {
			fmt.Println("Fetching team state from origin...")
			if err := refManager.FetchCaryaRefs("origin"); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: Could not fetch refs: %v\n", err)
			}
		}

		// Get current user ID
		userIdentity := identity.NewUserIdentity(repo.CaryaPath())
		currentUserID, _ := userIdentity.Get()

		// List all user refs
		refs, err := refManager.ListUserRefs()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error listing refs: %v\n", err)
			os.Exit(1)
		}

		if len(refs) == 0 {
			fmt.Println("No team members have published their working state yet.")
			fmt.Println("Use 'carya publish' to share your state.")
			return
		}

		fmt.Println("Team members with published working states:")
		fmt.Println()
		for _, ref := range refs {
			marker := "  "
			if ref.UserID == currentUserID {
				marker = "* "
			}
			fmt.Printf("%s%s\n", marker, ref.UserID)
			fmt.Printf("    Tree: %s\n", ref.TreeHash[:12])
		}
		fmt.Println()
		fmt.Println("* = you")
	},
}

var teamConflictsCmd = &cobra.Command{
	Use:   "conflicts <user>",
	Short: "Check for conflicts with a team member",
	Long: `Predict potential merge conflicts between your working state and another
team member's working state.

This uses git merge-tree to simulate a merge without actually modifying any files.`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		targetUser := args[0]

		repo := mustInitializedRepo()

		// Get current user's tree
		userIdentity := identity.NewUserIdentity(repo.CaryaPath())
		currentUserID, err := userIdentity.Get()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		refManager := git.NewRefManager(repo.RootPath())

		// Get both users' tree hashes
		myTree, err := refManager.GetUserTreeRef(currentUserID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: You haven't published your state yet. Run 'carya publish' first.\n")
			os.Exit(1)
		}

		theirTree, err := refManager.GetUserTreeRef(targetUser)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: User '%s' hasn't published their state.\n", targetUser)
			os.Exit(1)
		}

		// Get the base tree (HEAD)
		baseTree, err := refManager.GetHEADTreeHash()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error getting base tree: %v\n", err)
			os.Exit(1)
		}

		// Check for overlapping files first
		predictor := git.NewConflictPredictor(repo.RootPath())
		overlapping, err := predictor.OverlapsWith(baseTree, myTree, theirTree)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error checking overlaps: %v\n", err)
			os.Exit(1)
		}

		if len(overlapping) == 0 {
			fmt.Printf("No overlapping changes between you and %s.\n", targetUser)
			return
		}

		fmt.Printf("Files modified by both you and %s:\n", targetUser)
		for _, path := range overlapping {
			fmt.Printf("  - %s\n", path)
		}
		fmt.Println()

		// Predict actual conflicts
		report, err := predictor.PredictConflicts(baseTree, myTree, theirTree)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error predicting conflicts: %v\n", err)
			os.Exit(1)
		}

		if report.HasConflicts {
			fmt.Println("Potential merge conflicts detected:")
			for _, conflict := range report.ConflictedFiles {
				fmt.Printf("  - %s\n", conflict.Path)
			}
		} else {
			fmt.Println("No conflicts predicted - changes can be merged cleanly.")
		}
	},
}

var teamDiffCmd = &cobra.Command{
	Use:   "diff <user>",
	Short: "Show differences from another team member's state",
	Long:  `Show the files that differ between your working state and another team member's state.`,
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		targetUser := args[0]

		repo := mustInitializedRepo()

		// Get current user's tree
		userIdentity := identity.NewUserIdentity(repo.CaryaPath())
		currentUserID, err := userIdentity.Get()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		refManager := git.NewRefManager(repo.RootPath())

		// Get both users' tree hashes
		myTree, err := refManager.GetUserTreeRef(currentUserID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: You haven't published your state yet. Run 'carya publish' first.\n")
			os.Exit(1)
		}

		theirTree, err := refManager.GetUserTreeRef(targetUser)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: User '%s' hasn't published their state.\n", targetUser)
			os.Exit(1)
		}

		// Get diff between trees
		predictor := git.NewConflictPredictor(repo.RootPath())
		diffs, err := predictor.DiffTrees(myTree, theirTree)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error getting diff: %v\n", err)
			os.Exit(1)
		}

		if len(diffs) == 0 {
			fmt.Printf("Your working state matches %s's state.\n", targetUser)
			return
		}

		fmt.Printf("Differences between your state and %s's state:\n\n", targetUser)
		for _, d := range diffs {
			status := "?"
			switch d.Status {
			case "A":
				status = "added"
			case "D":
				status = "deleted"
			case "M":
				status = "modified"
			}
			fmt.Printf("  %s: %s\n", status, d.Path)
		}
	},
}

func init() {
	teamCmd.Flags().BoolVar(&fetchFlag, "fetch", false, "Fetch refs from origin first")
	teamCmd.AddCommand(teamConflictsCmd)
	teamCmd.AddCommand(teamDiffCmd)
	rootCmd.AddCommand(teamCmd)
}
