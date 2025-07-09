package main

import (
	"fmt"

	"github.com/spf13/cobra"
	"gurt/watcher"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize a new Gurt repository.",
	Long:  `Initializes a new Gurt repository in the current directory.`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Initializing Gurt repository...")
		watcher.Start(".")
	},
}

func init() {
	rootCmd.AddCommand(initCmd)
}
