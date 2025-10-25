package main

import (
	"fmt"
	"os"

	"carya/internal/repository"
	"carya/internal/tui"

	"github.com/spf13/cobra"
)

var viewCmd = &cobra.Command{
	Use:   "view",
	Short: "View tracked chunks and diffs",
	Long:  `View tracked chunks and diffs in an interactive TUI viewer.`,
	Run: func(cmd *cobra.Command, args []string) {
		dbPath, _ := cmd.Flags().GetString("db")

		// If no db path specified, use the default repository path
		if dbPath == "" {
			repo, err := repository.New()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error initializing repository: %v\n", err)
				os.Exit(1)
			}

			if !repo.Exists() {
				fmt.Fprintf(os.Stderr, "Error: Not a Carya repository. Run 'carya init' first.\n")
				os.Exit(1)
			}

			dbPath = repo.DBPath()
		}

		// Ensure the db file exists
		if _, err := os.Stat(dbPath); os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "Error: Database not found at %s\n", dbPath)
			os.Exit(1)
		}

		// Run the diff viewer
		if err := tui.RunDiffViewer(dbPath); err != nil {
			fmt.Fprintf(os.Stderr, "Error running diff viewer: %v\n", err)
			os.Exit(1)
		}
	},
}

func init() {
	// Add flags
	viewCmd.Flags().StringP("db", "d", "", "Path to the chunks database (default: .carya/chunks.db)")

	// Add to root command
	rootCmd.AddCommand(viewCmd)
}
