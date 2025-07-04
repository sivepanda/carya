package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "gurt",
	Short: "Gurt is a next-gen version control system.",
	Long:  `A fast and powerful version control system built with a focus on developer experience and collaboration.`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Gurt is running. Use 'gurt --help' for a list of commands.")
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func main() {
	Execute()
}
