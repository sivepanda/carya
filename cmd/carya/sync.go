package main

import (
	"fmt"
	"os"

	"carya/internal/chunk"
	"carya/internal/repository"
	"carya/internal/store"

	"github.com/spf13/cobra"
)

type syncResult struct {
	kept   int
	pruned int
}

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Reconcile stored chunks with git state",
	Long:  `Prune chunks that are already committed or no longer match current diffs.`,
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

		res, err := runChunkSync(repo.RootPath(), repo.DBPath())
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error syncing chunks: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("Chunk sync complete: kept %d, pruned %d\n", res.kept, res.pruned)
	},
}

func runChunkSync(repoPath, dbPath string) (syncResult, error) {
	s, err := store.NewSQLiteStore(dbPath)
	if err != nil {
		return syncResult{}, err
	}
	defer s.Close()

	chunks, err := s.GetAllChunks()
	if err != nil {
		return syncResult{}, err
	}

	var pruneIDs []chunk.ChunkID
	kept := 0
	for _, c := range chunks {
		decision, _ := chunk.DecideRetention(repoPath, c)
		if decision == chunk.DecisionPrune {
			pruneIDs = append(pruneIDs, c.ID)
			continue
		}
		kept++
	}

	if err := s.DeleteChunks(pruneIDs); err != nil {
		return syncResult{}, err
	}

	return syncResult{kept: kept, pruned: len(pruneIDs)}, nil
}

func init() {
	rootCmd.AddCommand(syncCmd)
}
