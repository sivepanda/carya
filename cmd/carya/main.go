// Package main provides the command-line interface for Carya, a next-generation
// version control system focused on developer experience and collaboration.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "carya",
	Short: "Carya is a next-gen version control system.",
	Long:  `A fast and powerful version control system built with a focus on developer experience and collaboration.`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Carya is running. Use 'carya --help' for a list of commands.")
	},
}

// Execute runs the root command and handles any errors that occur during execution.
// It prints errors to stderr and exits with code 1 if an error occurs.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// main is the entry point for the Carya CLI application.
func main() {
	Execute()
}
