package main

import (
	"fmt"
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

// nvimLspConfig is a self-contained Neovim plugin file. Because `filetypes`
// is omitted, the client attaches to all filetypes; the `.carya` root marker
// restricts it to Carya-initialized repositories.
const nvimLspConfig = `-- Carya LSP: team-conflict diagnostics (added by 'carya lsp setup nvim --write')
vim.lsp.config('carya', {
  cmd = { 'carya', 'lsp' },
  root_markers = { '.carya' },
})
vim.lsp.enable('carya')
`

const helixLspConfig = `[language-server.carya]
command = "carya"
args = ["lsp"]
required-root-patterns = [".carya"]
`

var lspSetupWrite bool

var lspSetupCmd = &cobra.Command{
	Use:   "setup [nvim|helix|vscode]",
	Short: "Show or install editor configuration for the Carya LSP",
	Long: `Print the editor configuration needed to connect to the Carya LSP server.

With --write (Neovim only), installs the config automatically into your
Neovim plugin directory so it loads on startup.`,
	Args:      cobra.MaximumNArgs(1),
	ValidArgs: []string{"nvim", "helix", "vscode"},
	Run: func(cmd *cobra.Command, args []string) {
		editor := ""
		if len(args) > 0 {
			editor = args[0]
		}

		switch editor {
		case "nvim":
			if lspSetupWrite {
				path, err := writeNvimConfig()
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error: %v\n", err)
					os.Exit(1)
				}
				fmt.Printf("Wrote %s\n", path)
				fmt.Println("The Carya LSP will attach automatically in Carya repositories (requires Neovim 0.11+).")
				return
			}
			fmt.Println("Add to your Neovim config (requires Neovim 0.11+), or run 'carya lsp setup nvim --write':")
			fmt.Println()
			fmt.Print(nvimLspConfig)
		case "helix":
			fmt.Println("Add to .helix/languages.toml (project) or ~/.config/helix/languages.toml (global):")
			fmt.Println()
			fmt.Print(helixLspConfig)
			fmt.Println()
			fmt.Println(`Helix attaches language servers per-language, so also add "carya" to the`)
			fmt.Println("language-servers list of each language you want diagnostics for, e.g.:")
			fmt.Println()
			fmt.Println("  [[language]]")
			fmt.Println("  name = \"go\"")
			fmt.Println("  language-servers = [\"gopls\", \"carya\"]")
		case "vscode":
			fmt.Println("VS Code can only launch language servers through an extension; there is no")
			fmt.Println("config-only way to register one. A Carya VS Code extension is not available yet.")
		default:
			fmt.Println("Carya ships an LSP server ('carya lsp') that surfaces team conflicts as diagnostics.")
			fmt.Println()
			fmt.Println("Supported setups:")
			fmt.Println("  carya lsp setup nvim           print Neovim config (0.11+)")
			fmt.Println("  carya lsp setup nvim --write   install it into your Neovim plugin directory")
			fmt.Println("  carya lsp setup helix          print Helix languages.toml config")
			fmt.Println("  carya lsp setup vscode         VS Code status")
		}
	},
}

// writeNvimConfig installs the Carya LSP config as a Neovim plugin file, which
// Neovim sources automatically on startup — no init.lua edits needed.
func writeNvimConfig() (string, error) {
	configDir := os.Getenv("XDG_CONFIG_HOME")
	if configDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("locate home directory: %w", err)
		}
		configDir = filepath.Join(home, ".config")
	}

	pluginDir := filepath.Join(configDir, "nvim", "plugin")
	path := filepath.Join(pluginDir, "carya-lsp.lua")

	if existing, err := os.ReadFile(path); err == nil {
		if string(existing) == nvimLspConfig {
			return path, nil
		}
		return "", fmt.Errorf("%s already exists with different content; remove it first or configure manually", path)
	}

	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		return "", fmt.Errorf("create %s: %w", pluginDir, err)
	}
	if err := os.WriteFile(path, []byte(nvimLspConfig), 0644); err != nil {
		return "", fmt.Errorf("write %s: %w", path, err)
	}
	return path, nil
}

func init() {
	lspSetupCmd.Flags().BoolVar(&lspSetupWrite, "write", false, "Install the config instead of printing it (Neovim only)")
	lspCmd.AddCommand(lspSetupCmd)
	rootCmd.AddCommand(lspCmd)
}
