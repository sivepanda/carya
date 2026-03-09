package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"carya/internal/config"
	"carya/internal/daemon"
	"carya/internal/features/engine"
	"carya/internal/features/watcher"
	"carya/internal/git"
	"carya/internal/repository"

	"github.com/spf13/cobra"
)

type DaemonStatus struct {
	FlushInterval string `json:"flush_interval"`
	IsIdle        bool   `json:"is_idle"`
	LastUpdate    string `json:"last_update"`
}

var daemonCmd = &cobra.Command{
	Use:    "daemon",
	Short:  "Run Carya watcher as a background daemon",
	Hidden: true,
	Run: func(cmd *cobra.Command, args []string) {
		repo, err := repository.New()
		if err != nil {
			log.Fatalf("Failed to initialize repository: %v", err)
		}

		if !repo.Exists() {
			log.Fatalf("Not a Carya repository. Run 'carya init' first.")
		}

		d := daemon.New(repo.PIDPath(), repo.LogPath())
		if err := d.WritePID(); err != nil {
			log.Fatalf("Failed to write PID file: %v", err)
		}
		defer d.RemovePID()

		log.Println("Starting Carya daemon...")

		teamCfg := config.LoadTeamConfig(repo.CaryaPath())
		refManager := git.NewRefManager(repo.RootPath())

		if teamCfg.AutoFetch {
			if err := refManager.FetchCaryaRefs("origin"); err != nil {
				log.Printf("Note: Could not fetch team refs from origin: %v", err)
			} else {
				log.Println("Fetched team refs from origin")
			}
		}

		if err := refManager.SetBaseRef(); err != nil {
			log.Printf("Note: Could not set base ref: %v", err)
		}

		engineFeature := engine.NewEngineFeature()
		if err := engineFeature.Initialize(repo); err != nil {
			log.Fatalf("Failed to initialize engine: %v", err)
		}

		watcherFeature := watcher.NewWatcherFeature()
		if err := watcherFeature.InitializeWithEngine(repo, engineFeature.Engine()); err != nil {
			log.Fatalf("Failed to initialize watcher: %v", err)
		}

		if err := engineFeature.Start(); err != nil {
			log.Fatalf("Failed to start engine: %v", err)
		}
		defer engineFeature.Stop()

		if err := watcherFeature.Start(); err != nil {
			log.Fatalf("Failed to start watcher: %v", err)
		}
		defer watcherFeature.Stop()

		log.Println("Carya daemon is now watching for file changes")

		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM, syscall.SIGUSR1)

		var fetchInProgress int32

		go func() {
			statusTicker := time.NewTicker(10 * time.Second)
			publishTicker := time.NewTicker(60 * time.Second)
			fetchTicker := time.NewTicker(2 * time.Minute)
			defer statusTicker.Stop()
			defer publishTicker.Stop()
			defer fetchTicker.Stop()

			for {
				select {
				case <-statusTicker.C:
					interval, isIdle := engineFeature.Engine().FlushStatus()
					status := DaemonStatus{
						FlushInterval: interval.String(),
						IsIdle:        isIdle,
						LastUpdate:    time.Now().Format(time.RFC3339),
					}
					data, err := json.Marshal(status)
					if err != nil {
						continue
					}
					os.WriteFile(repo.StatusPath(), data, 0644)

				case <-publishTicker.C:
					if !teamCfg.AutoPublish {
						continue
					}
					engineFeature.Engine().FlushAll()
					if err := engineFeature.Engine().PublishState(); err != nil {
						log.Printf("Auto-publish: %v", err)
					} else {
						log.Println("Auto-published working state")
					}

				case <-fetchTicker.C:
					if !teamCfg.AutoFetch {
						continue
					}
					if !atomic.CompareAndSwapInt32(&fetchInProgress, 0, 1) {
						continue
					}
					go func() {
						defer atomic.StoreInt32(&fetchInProgress, 0)
						if err := refManager.FetchCaryaRefs("origin"); err != nil {
							log.Printf("Auto-fetch: %v", err)
						}
					}()

				case <-sigCh:
					return
				}
			}
		}()

		for sig := range sigCh {
			switch sig {
			case syscall.SIGUSR1:
				log.Println("Received flush signal, flushing all chunks...")
				if err := engineFeature.Engine().FlushAll(); err != nil {
					log.Printf("Error flushing chunks: %v", err)
				} else {
					log.Println("All chunks flushed successfully")
				}
				if teamCfg.AutoPublish {
					if err := engineFeature.Engine().PublishState(); err != nil {
						log.Printf("Error publishing state: %v", err)
					}
				}
			case os.Interrupt, syscall.SIGTERM:
				log.Println("Shutting down Carya daemon...")
				engineFeature.Engine().FlushAll()
				if teamCfg.AutoPublish {
					engineFeature.Engine().PublishState()
				}
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

			statusData, err := os.ReadFile(repo.StatusPath())
			if err == nil {
				var status DaemonStatus
				if err := json.Unmarshal(statusData, &status); err == nil {
					mode := "active"
					if status.IsIdle {
						mode = "backing off"
					}
					fmt.Printf("  Flush interval: %s (%s)\n", status.FlushInterval, mode)
				}
			}
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
