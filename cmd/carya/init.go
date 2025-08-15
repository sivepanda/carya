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
		if _, err := p.Run(); err != nil {
			fmt.Printf("Alas, there's been an error")
			os.Exit(1)
		}
	},
}

func init() {
	rootCmd.AddCommand(initCmd)
}
