package main

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"
)

var pushCmd = &cobra.Command{
	Use:   "push [git-push-args...]",
	Short: "Push to git and sync chunks",
	Long:  `Run git push, then reconcile Carya chunks against the updated git state.`,
	Run: func(cmd *cobra.Command, args []string) {
		repo := mustInitializedRepo()

		gitArgs := append([]string{"push"}, args...)
		gitPush := exec.Command("git", gitArgs...)
		gitPush.Dir = repo.RootPath()
		gitPush.Stdout = os.Stdout
		gitPush.Stderr = os.Stderr

		if err := gitPush.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: git push failed: %v\n", err)
			os.Exit(1)
		}

		res, err := runChunkSync(repo.RootPath(), repo.DBPath())
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: git push succeeded but chunk sync failed: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("Chunk sync complete: kept %d, pruned %d\n", res.kept, res.pruned)
	},
}

func init() {
	rootCmd.AddCommand(pushCmd)
}
