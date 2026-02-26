package main

import (
	"log"
	"os"
	"path/filepath"

	"carya/internal/lsp"
	"carya/internal/repository"

	"github.com/spf13/cobra"
)

var lspCmd = &cobra.Command{
	Use:   "lsp",
	Short: "Start the Carya LSP server for team conflict diagnostics",
	Long:  `Start a Language Server Protocol server that surfaces team conflicts as diagnostics in your editor. Hover over warnings to see teammate diffs.`,
	Run: func(cmd *cobra.Command, args []string) {
		repo, err := repository.New()
		if err == nil && repo.Exists() {
			logPath := filepath.Join(repo.CaryaPath(), "lsp.log")
			if f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644); err == nil {
				defer f.Close()
				log.SetOutput(f)
			}
		}

		server := lsp.NewServer(os.Stdin, os.Stdout)
		if err := server.Run(); err != nil {
			log.Fatalf("lsp server: %v", err)
		}
	},
}

func init() {
	rootCmd.AddCommand(lspCmd)
}
