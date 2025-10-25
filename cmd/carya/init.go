package main

import (
	"fmt"
	"os"

	"carya/internal/tui"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

// command itself
var initCmd = &cobra.Command{
	Use:   "init",
	Short: "initialize a new Carya repository.",
	Long:  `initialize a new Carya repository in the current directory and starts watching for file changes.`,
	Run: func(cmd *cobra.Command, args []string) {
		// Create initializer
		// Create and run the TUI model
		model := tui.NewInitModel()
		p := tea.NewProgram(&model)
		finalModel, err := p.Run()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error running initialization: %v\n", err)
			os.Exit(1)
		}

		// Check if we should launch housekeeping setup
		if initModel, ok := finalModel.(*tui.InitModel); ok {
			if initModel.ShouldLaunchHousekeeping() {
				// Launch housekeeping TUI
				housekeepingModel := tui.NewHousekeepingModel()
				p := tea.NewProgram(housekeepingModel)
				if _, err := p.Run(); err != nil {
					fmt.Fprintf(os.Stderr, "Error running housekeeping setup: %v\n", err)
					os.Exit(1)
				}
			}
		}
	},
}

func init() {
	rootCmd.AddCommand(initCmd)
}
