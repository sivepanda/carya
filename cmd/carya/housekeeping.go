package main

import (
	"fmt"
	"strings"

	"carya/internal/housekeeping"
	"carya/internal/tui"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

var housekeepingCmd = &cobra.Command{
	Use:   "housekeeping",
	Short: "Manage housekeeping commands",
	Long:  `Manage housekeeping commands that run automatically after git operations like pull and checkout.`,
	Run: func(cmd *cobra.Command, args []string) {
		// Run interactive TUI by default
		m := tui.NewHousekeepingModel()
		p := tea.NewProgram(m)
		if _, err := p.Run(); err != nil {
			fmt.Printf("Error running interactive setup: %v\n", err)
		}
	},
}

var housekeepingAddCmd = &cobra.Command{
	Use:   "add [command]",
	Short: "Add a housekeeping command",
	Long:  `Add a housekeeping command to run after git operations.`,
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		command := args[0]

		postPull, _ := cmd.Flags().GetBool("post-pull")
		postCheckout, _ := cmd.Flags().GetBool("post-checkout")
		workingDir, _ := cmd.Flags().GetString("working-dir")
		description, _ := cmd.Flags().GetString("description")

		if !postPull && !postCheckout {
			fmt.Println("Error: Must specify either --post-pull or --post-checkout")
			return
		}

		if postPull && postCheckout {
			fmt.Println("Error: Cannot specify both --post-pull and --post-checkout")
			return
		}

		config, err := housekeeping.LoadConfig()
		if err != nil {
			fmt.Printf("Error loading config: %v\n", err)
			return
		}

		if workingDir == "" {
			workingDir = "."
		}

		if description == "" {
			description = command
		}

		var category string
		if postPull {
			category = "post-pull"
		} else {
			category = "post-checkout"
		}

		if err := config.AddCommand(category, command, workingDir, description); err != nil {
			fmt.Printf("Error adding command: %v\n", err)
			return
		}

		if err := config.Save(); err != nil {
			fmt.Printf("Error saving config: %v\n", err)
			return
		}

		fmt.Printf("Added %s command: %s\n", category, command)
	},
}

var housekeepingListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all housekeeping commands",
	Long:  `List all configured housekeeping commands by category.`,
	Run: func(cmd *cobra.Command, args []string) {
		config, err := housekeeping.LoadConfig()
		if err != nil {
			fmt.Printf("Error loading config: %v\n", err)
			return
		}

		categories := []string{"post-pull", "post-checkout"}

		for _, category := range categories {
			commands, err := config.GetCommands(category)
			if err != nil {
				fmt.Printf("Error getting %s commands: %v\n", category, err)
				continue
			}

			fmt.Printf("\n%s commands:\n", strings.Title(strings.ReplaceAll(category, "-", " ")))
			if len(commands) == 0 {
				fmt.Println("  (none)")
			} else {
				for i, cmd := range commands {
					fmt.Printf("  %d. %s\n", i+1, cmd.Description)
					fmt.Printf("     Command: %s\n", cmd.Command)
					if cmd.WorkingDir != "." && cmd.WorkingDir != "" {
						fmt.Printf("     Working Dir: %s\n", cmd.WorkingDir)
					}
				}
			}
		}
	},
}

var housekeepingEditCmd = &cobra.Command{
	Use:   "edit",
	Short: "Edit the housekeeping configuration file",
	Long:  `Open the housekeeping configuration file in your preferred editor.`,
	Run: func(cmd *cobra.Command, args []string) {
		if err := housekeeping.OpenConfigInEditor(); err != nil {
			fmt.Printf("Error opening config in editor: %v\n", err)
			return
		}
	},
}

var housekeepingRunCmd = &cobra.Command{
	Use:   "run [category]",
	Short: "Run housekeeping commands for a specific category",
	Long:  `Run housekeeping commands for a specific category (post-pull or post-checkout).`,
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		category := args[0]
		autoApprove, _ := cmd.Flags().GetBool("auto")

		if category != "post-pull" && category != "post-checkout" {
			fmt.Printf("Error: Invalid category '%s'. Must be 'post-pull' or 'post-checkout'\n", category)
			return
		}

		config, err := housekeeping.LoadConfig()
		if err != nil {
			fmt.Printf("Error loading config: %v\n", err)
			return
		}

		executor := housekeeping.NewExecutor(config)
		if err := executor.ExecuteCategory(category, autoApprove); err != nil {
			fmt.Printf("Error executing %s commands: %v\n", category, err)
			return
		}
	},
}

var housekeepingDetectCmd = &cobra.Command{
	Use:   "detect",
	Short: "Detect package managers and build systems in the project",
	Long:  `Scan the project directory to detect package managers and build systems.`,
	Run: func(cmd *cobra.Command, args []string) {
		detector := housekeeping.NewDetector(".")
		detected, err := detector.DetectPackages()
		if err != nil {
			fmt.Printf("Error detecting packages: %v\n", err)
			return
		}

		if len(detected) == 0 {
			fmt.Println("No package managers or build systems detected.")
			return
		}

		fmt.Println("Detected package managers and build systems:")
		for _, pkg := range detected {
			fmt.Printf("  • %s (%s)\n", pkg.Type.Description, pkg.Path)
		}
	},
}

var housekeepingSuggestCmd = &cobra.Command{
	Use:   "suggest [category]",
	Short: "Suggest housekeeping commands based on detected packages",
	Long:  `Automatically suggest housekeeping commands based on detected package managers and build systems.`,
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		category := args[0]

		if category != "post-pull" && category != "post-checkout" {
			fmt.Printf("Error: Invalid category '%s'. Must be 'post-pull' or 'post-checkout'\n", category)
			return
		}

		detector := housekeeping.NewDetector(".")
		suggestions, err := detector.GetSuggestedCommands(category)
		if err != nil {
			fmt.Printf("Error getting suggestions: %v\n", err)
			return
		}

		if len(suggestions) == 0 {
			fmt.Printf("No suggestions for %s commands.\n", category)
			return
		}

		fmt.Printf("Suggested %s commands:\n", category)
		for i, suggestion := range suggestions {
			fmt.Printf("  %d. %s\n", i+1, suggestion.Description)
			fmt.Printf("     Command: %s\n", suggestion.Command)
		}

		fmt.Println("\nTo add these commands, use:")
		fmt.Printf("  carya housekeeping auto %s\n", category)
	},
}

var housekeepingAutoCmd = &cobra.Command{
	Use:   "auto [category]",
	Short: "Automatically add suggested housekeeping commands",
	Long:  `Automatically detect and add suggested housekeeping commands based on your project's package managers.`,
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		category := args[0]

		if category != "post-pull" && category != "post-checkout" {
			fmt.Printf("Error: Invalid category '%s'. Must be 'post-pull' or 'post-checkout'\n", category)
			return
		}

		detector := housekeeping.NewDetector(".")
		suggestions, err := detector.GetSuggestedCommands(category)
		if err != nil {
			fmt.Printf("Error getting suggestions: %v\n", err)
			return
		}

		if len(suggestions) == 0 {
			fmt.Printf("No suggestions for %s commands.\n", category)
			return
		}

		config, err := housekeeping.LoadConfig()
		if err != nil {
			fmt.Printf("Error loading config: %v\n", err)
			return
		}

		fmt.Printf("Adding %d suggested %s commands:\n", len(suggestions), category)
		for _, suggestion := range suggestions {
			fmt.Printf("  • %s\n", suggestion.Description)
			if err := config.AddCommand(category, suggestion.Command, suggestion.WorkingDir, suggestion.Description); err != nil {
				fmt.Printf("Error adding command: %v\n", err)
				return
			}
		}

		if err := config.Save(); err != nil {
			fmt.Printf("Error saving config: %v\n", err)
			return
		}

		fmt.Printf("\nSuccessfully added %d %s commands!\n", len(suggestions), category)
	},
}

var housekeepingSetupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Interactive setup for housekeeping commands",
	Long:  `Launch an interactive UI to detect package managers and select which housekeeping commands to add.`,
	Run: func(cmd *cobra.Command, args []string) {
		m := tui.NewHousekeepingModel()
		p := tea.NewProgram(m)
		if _, err := p.Run(); err != nil {
			fmt.Printf("Error running interactive setup: %v\n", err)
		}
	},
}

func init() {
	// Add flags to the add command
	housekeepingAddCmd.Flags().Bool("post-pull", false, "Add command to post-pull category")
	housekeepingAddCmd.Flags().Bool("post-checkout", false, "Add command to post-checkout category")
	housekeepingAddCmd.Flags().StringP("working-dir", "d", ".", "Working directory for the command")
	housekeepingAddCmd.Flags().StringP("description", "m", "", "Description of the command")

	// Add flags to the run command
	housekeepingRunCmd.Flags().Bool("auto", false, "Run commands without confirmation")

	// Add subcommands to housekeeping
	housekeepingCmd.AddCommand(housekeepingSetupCmd)
	housekeepingCmd.AddCommand(housekeepingAddCmd)
	housekeepingCmd.AddCommand(housekeepingListCmd)
	housekeepingCmd.AddCommand(housekeepingEditCmd)
	housekeepingCmd.AddCommand(housekeepingRunCmd)
	housekeepingCmd.AddCommand(housekeepingDetectCmd)
	housekeepingCmd.AddCommand(housekeepingSuggestCmd)
	housekeepingCmd.AddCommand(housekeepingAutoCmd)

	// Add housekeeping to root command
	rootCmd.AddCommand(housekeepingCmd)
}
