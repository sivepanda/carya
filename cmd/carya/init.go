package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"carya/internal/core"
	"carya/internal/watcher"

	"github.com/spf13/cobra"
)

// lorem ipsum dolor

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize a new Carya repository.",
	Long:  `Initializes a new Carya repository in the current directory and starts watching for file changes.`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Initializing Carya repository...")

		// Create .carya directory for storing data
		caryaDir := ".carya"
		if err := os.MkdirAll(caryaDir, 0755); err != nil {
			log.Fatal("Failed to create .carya directory:", err)
		}

		// Initialize the core engine
		dbPath := filepath.Join(caryaDir, "chunks.db")
		engine, err := core.NewEngine(dbPath)
		if err != nil {
			log.Fatal("Failed to initialize engine:", err)
		}

		// Start the engine
		engine.Start()
		defer engine.Stop()

		// Create and start the file watcher
		fileWatcher, err := watcher.New(engine)
		if err != nil {
			log.Fatal("Failed to create watcher:", err)
		}

		if err := fileWatcher.Start("."); err != nil {
			log.Fatal("Failed to start watcher:", err)
		}
		defer fileWatcher.Stop()

		fmt.Println("Carya is now watching for file changes...")
		fmt.Println("Press Ctrl+C to stop")

		// Wait for interrupt signal
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt, syscall.SIGTERM)
		<-c

		fmt.Println("\nShutting down Carya...")
	},
}

func init() {
	rootCmd.AddCommand(initCmd)
}
