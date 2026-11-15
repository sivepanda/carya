package main

import (
	"fmt"
	"os"

	"carya/internal/daemon"
	"carya/internal/repository"
	"carya/internal/tui/model"

	tea "charm.land/bubbletea/v2"
	"github.com/spf13/cobra"
)

// command itself
var initCmd = &cobra.Command{
	Use:   "init",
	Short: "initialize a new Carya repository.",
	Long:  `initialize a new Carya repository in the current directory and starts watching for file changes.`,
	Run: func(cmd *cobra.Command, args []string) {
		// Carya builds on an existing git repository; fail fast with a clear
		// message instead of letting the TUI's feature setup silently no-op.
		if _, err := repository.New(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		// Create and run the TUI model
		initModel := model.NewInit()
		p := tea.NewProgram(&initModel)
		finalModel, err := p.Run()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error running initialization: %v\n", err)
			os.Exit(1)
		}

		// Check if we should launch housekeeping setup or start daemon
		if initModel, ok := finalModel.(*model.Init); ok {
			if initModel.ShouldLaunchHousekeeping() {
				// Launch housekeeping TUI
				housekeepingModel := model.NewHousekeeping()
				p := tea.NewProgram(housekeepingModel)
				if _, err := p.Run(); err != nil {
					fmt.Fprintf(os.Stderr, "Error running housekeeping setup: %v\n", err)
					os.Exit(1)
				}
			}

			// Start the daemon if featcom is enabled and initialized successfully
			if initModel.IsFeatureEnabled("featcom") && !initModel.HasFeatureError("featcom") {
				fmt.Println("\nStarting Carya daemon...")

				repo, err := repository.New()
				if err != nil {
					fmt.Fprintf(os.Stderr, "Warning: Failed to initialize repository: %v\n", err)
					fmt.Fprintf(os.Stderr, "You can manually start it later with 'carya start'\n")
				} else {
					d := daemon.New(repo.PIDPath(), repo.LogPath())

					if d.IsRunning() {
						fmt.Println("Carya daemon is already running")
					} else if err := d.Start([]string{"daemon"}); err != nil {
						fmt.Fprintf(os.Stderr, "Warning: Failed to start daemon: %v\n", err)
						fmt.Fprintf(os.Stderr, "You can manually start it later with 'carya start'\n")
					} else {
						fmt.Println("✓ Carya daemon started")
						fmt.Printf("  Log file: %s\n", d.GetLogPath())
					}
				}
			}
		}
	},
}

func init() {
	rootCmd.AddCommand(initCmd)
}
