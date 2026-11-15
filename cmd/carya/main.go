// Package main provides the command-line interface for Carya, a next-generation
// version control system focused on developer experience and collaboration.
package main

import (
	"fmt"
	"io"
	"log"
	"os"

	"carya/internal/repository"

	"github.com/sivepanda/mycelia"
	"github.com/spf13/cobra"
)

var globalLogFile *os.File

var rootCmd = &cobra.Command{
	Use:   "carya",
	Short: "Carya is a next-gen version control system.",
	Long:  `A fast and powerful version control system built with a focus on developer experience and collaboration.`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		repo, err := repository.New()
		if err != nil {
			return nil
		}

		if !repo.Exists() {
			if cmd.Name() != "init" {
				return nil
			}
			if err := repo.EnsureExists(); err != nil {
				return fmt.Errorf("failed to initialize log directory: %w", err)
			}
		}

		f, err := os.OpenFile(repo.LogPath(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return fmt.Errorf("failed to open log file: %w", err)
		}
		globalLogFile = f
		log.SetOutput(f)
		return nil
	},
	PersistentPostRun: func(cmd *cobra.Command, args []string) {
		if globalLogFile != nil {
			globalLogFile.Close()
			globalLogFile = nil
			log.SetOutput(io.Discard)
		}
	},
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
func init() {
	mycelia.ConfigFile = "carya.json"
	log.SetOutput(io.Discard)
}

func main() {
	Execute()
}
