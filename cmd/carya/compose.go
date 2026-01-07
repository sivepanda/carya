package main

import (
	"fmt"
	"log"
	"os"

	"carya/internal/repository"
	"carya/internal/tui"

	"github.com/spf13/cobra"
)

var composeCmd = &cobra.Command{
	Use:   "compose",
	Short: "Select diffs and compose a commit",
	Long:  `Select specific diffs from tracked chunks and compose a Git commit from them.`,
	Run: func(cmd *cobra.Command, args []string) {
		dbPath, _ := cmd.Flags().GetString("db")

		// Set up repository for default db path and logging
		repo, err := repository.New()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error initializing repository: %v\n", err)
			os.Exit(1)
		}

		if !repo.Exists() {
			fmt.Fprintf(os.Stderr, "Error: Not a Carya repository. Run 'carya init' first.\n")
			os.Exit(1)
		}
		
		// Set up logging immediately
		logFile, err := os.OpenFile(repo.LogPath(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to open log file: %v\n", err)
		} else {
			defer logFile.Close()
			log.SetOutput(logFile)
			log.Println("===== Compose command started =====")
		}

		// If no db path specified, use the default repository path
		if dbPath == "" {
			dbPath = repo.DBPath()
			log.Printf("Using default DB path: %s", dbPath)
		} else {
			log.Printf("Using specified DB path: %s", dbPath)
		}

		// Ensure the db file exists
		if _, err := os.Stat(dbPath); os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "Error: Database not found at %s\n", dbPath)
			os.Exit(1)
		}

		// Run the commit composer
		if err := tui.RunCommitComposer(dbPath); err != nil {
			log.Printf("Error running commit composer: %v", err)
			fmt.Fprintf(os.Stderr, "Error running commit composer: %v\n", err)
			os.Exit(1)
		}
		
		log.Println("===== Compose command completed =====")
	},
}

func init() {
	// Add flags
	composeCmd.Flags().StringP("db", "d", "", "Path to the chunks database (default: .carya/chunks.db)")

	// Add to root command
	rootCmd.AddCommand(composeCmd)
}