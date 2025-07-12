package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/spf13/cobra"
	"gurt/internal/core"
	"gurt/internal/watcher"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize a new Gurt repository.",
	Long:  `Initializes a new Gurt repository in the current directory and starts watching for file changes.`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Initializing Gurt repository...")

		// Create .gurt directory for storing data
		gurtDir := ".gurt"
		if err := os.MkdirAll(gurtDir, 0755); err != nil {
			log.Fatal("Failed to create .gurt directory:", err)
		}

		// Initialize the core engine
		dbPath := filepath.Join(gurtDir, "chunks.db")
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

		fmt.Println("Gurt is now watching for file changes...")
		fmt.Println("Press Ctrl+C to stop")

		// Wait for interrupt signal
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt, syscall.SIGTERM)
		<-c

		fmt.Println("\nShutting down Gurt...")
	},
}

func init() {
	rootCmd.AddCommand(initCmd)
}
