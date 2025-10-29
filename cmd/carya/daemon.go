package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"carya/internal/daemon"
	"carya/internal/features/engine"
	"carya/internal/features/watcher"
	"carya/internal/repository"

	"github.com/spf13/cobra"
)

var daemonCmd = &cobra.Command{
	Use:    "daemon",
	Short:  "Run Carya watcher as a background daemon",
	Hidden: true, // Hidden from normal help - used internally
	Run: func(cmd *cobra.Command, args []string) {
		repo, err := repository.New()
		if err != nil {
			log.Fatalf("Failed to initialize repository: %v", err)
		}

		if !repo.Exists() {
			log.Fatalf("Not a Carya repository. Run 'carya init' first.")
		}

		// Create daemon manager
		d := daemon.New(
			repo.PIDPath(),
			repo.LogPath(),
		)

		// Write PID file
		if err := d.WritePID(); err != nil {
			log.Fatalf("Failed to write PID file: %v", err)
		}

		// Ensure PID file is removed on exit
		defer d.RemovePID()

		// Redirect logs to file
		logFile, err := os.OpenFile(repo.LogPath(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			log.Fatalf("Failed to open log file: %v", err)
		}
		defer logFile.Close()
		log.SetOutput(logFile)

		log.Println("Starting Carya daemon...")

		// Initialize engine feature
		engineFeature := engine.NewEngineFeature()
		if err := engineFeature.Initialize(repo); err != nil {
			log.Fatalf("Failed to initialize engine: %v", err)
		}

		// Initialize watcher feature with engine
		watcherFeature := watcher.NewWatcherFeature()
		if err := watcherFeature.InitializeWithEngine(repo, engineFeature.Engine()); err != nil {
			log.Fatalf("Failed to initialize watcher: %v", err)
		}

		// Start engine
		if err := engineFeature.Start(); err != nil {
			log.Fatalf("Failed to start engine: %v", err)
		}
		defer engineFeature.Stop()

		// Start watcher
		if err := watcherFeature.Start(); err != nil {
			log.Fatalf("Failed to start watcher: %v", err)
		}
		defer watcherFeature.Stop()

		log.Println("Carya daemon is now watching for file changes")

		// Set up signal handling
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM, syscall.SIGUSR1)

		// Wait for signals
		for sig := range sigCh {
			switch sig {
			case syscall.SIGUSR1:
				// Manual flush requested
				log.Println("Received flush signal, flushing all chunks...")
				if err := engineFeature.Engine().FlushAll(); err != nil {
					log.Printf("Error flushing chunks: %v", err)
				} else {
					log.Println("All chunks flushed successfully")
				}
			case os.Interrupt, syscall.SIGTERM:
				// Shutdown requested
				log.Println("Shutting down Carya daemon...")
				return
			}
		}
	},
}

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start Carya watcher in the background",
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

		d := daemon.New(repo.PIDPath(), repo.LogPath())

		if d.IsRunning() {
			fmt.Println("Carya daemon is already running")
			os.Exit(0)
		}

		// Start daemon in background
		if err := d.Start([]string{"daemon"}); err != nil {
			fmt.Fprintf(os.Stderr, "Error starting daemon: %v\n", err)
			os.Exit(1)
		}

		fmt.Println("✓ Carya daemon started")
		fmt.Printf("  Log file: %s\n", d.GetLogPath())
	},
}

var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the Carya watcher daemon",
	Run: func(cmd *cobra.Command, args []string) {
		repo, err := repository.New()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		d := daemon.New(repo.PIDPath(), repo.LogPath())

		if !d.IsRunning() {
			fmt.Println("Carya daemon is not running")
			os.Exit(0)
		}

		if err := d.Stop(); err != nil {
			fmt.Fprintf(os.Stderr, "Error stopping daemon: %v\n", err)
			os.Exit(1)
		}

		fmt.Println("✓ Carya daemon stopped")
	},
}

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check if Carya watcher is running",
	Run: func(cmd *cobra.Command, args []string) {
		repo, err := repository.New()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		d := daemon.New(repo.PIDPath(), repo.LogPath())

		if d.IsRunning() {
			pid, _ := d.ReadPID()
			fmt.Printf("✓ Carya daemon is running (PID: %d)\n", pid)
			fmt.Printf("  Log file: %s\n", d.GetLogPath())
		} else {
			fmt.Println("Carya daemon is not running")
		}
	},
}

var flushCmd = &cobra.Command{
	Use:   "flush",
	Short: "Flush all pending chunks to storage",
	Run: func(cmd *cobra.Command, args []string) {
		repo, err := repository.New()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		d := daemon.New(repo.PIDPath(), repo.LogPath())

		if !d.IsRunning() {
			fmt.Println("Carya daemon is not running")
			os.Exit(1)
		}

		pid, err := d.ReadPID()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading PID: %v\n", err)
			os.Exit(1)
		}

		process, err := os.FindProcess(pid)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error finding process: %v\n", err)
			os.Exit(1)
		}

		if err := process.Signal(syscall.SIGUSR1); err != nil {
			fmt.Fprintf(os.Stderr, "Error sending flush signal: %v\n", err)
			os.Exit(1)
		}

		fmt.Println("✓ Flush signal sent to daemon")
		fmt.Printf("  Check log file for results: %s\n", d.GetLogPath())
	},
}

func init() {
	rootCmd.AddCommand(daemonCmd)
	rootCmd.AddCommand(startCmd)
	rootCmd.AddCommand(stopCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(flushCmd)
}
