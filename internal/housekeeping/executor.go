package housekeeping

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Executor struct {
	config *Config
}

func NewExecutor(config *Config) *Executor {
	return &Executor{config: config}
}

func (e *Executor) ExecuteCategory(category string, autoApprove bool) error {
	return e.ExecuteCategoryWithChangedFiles(category, nil, autoApprove)
}

func (e *Executor) ExecuteCategoryWithChangedFiles(category string, changedFiles []string, autoApprove bool) error {
	commands, err := e.config.GetCommands(category)
	if err != nil {
		return err
	}

	// Filter commands to only those whose trigger files were changed
	if len(changedFiles) > 0 {
		commands = filterByTriggerFiles(commands, changedFiles)
	}

	if len(commands) == 0 {
		fmt.Printf("No %s commands to run.\n", category)
		return nil
	}

	fmt.Printf("Found %d %s tasks:\n", len(commands), category)
	for _, cmd := range commands {
		desc := cmd.Description
		if desc == "" {
			desc = cmd.Command
		}
		fmt.Printf("  • %s\n", desc)
	}

	if !autoApprove {
		fmt.Print("Run these? [Y/n]: ")
		var response string
		fmt.Scanln(&response)
		response = strings.TrimSpace(strings.ToLower(response))
		if response == "n" || response == "no" {
			fmt.Println("Skipped housekeeping tasks.")
			return nil
		}
	}

	fmt.Println("Running housekeeping tasks...")
	for i, cmd := range commands {
		fmt.Printf("[%d/%d] %s\n", i+1, len(commands), cmd.Description)
		if err := e.executeCommand(cmd); err != nil {
			return fmt.Errorf("failed to execute command '%s': %w", cmd.Command, err)
		}
	}

	fmt.Println("All housekeeping tasks completed successfully!")
	return nil
}

// filterByTriggerFiles returns only commands whose trigger files appear in the changed files list.
// Commands with no trigger files always run.
func filterByTriggerFiles(commands []Command, changedFiles []string) []Command {
	var filtered []Command
	for _, cmd := range commands {
		if len(cmd.TriggerFiles) == 0 {
			// No trigger files specified — always run
			filtered = append(filtered, cmd)
			continue
		}

		for _, trigger := range cmd.TriggerFiles {
			if matchesAnyChangedFile(trigger, changedFiles) {
				filtered = append(filtered, cmd)
				break
			}
		}
	}
	return filtered
}

// matchesAnyChangedFile checks if a trigger file pattern matches any file in the changed list.
func matchesAnyChangedFile(trigger string, changedFiles []string) bool {
	for _, changed := range changedFiles {
		// Exact match
		if changed == trigger {
			return true
		}
		// Glob match (e.g. "*.csproj", "prisma/schema.prisma")
		if strings.Contains(trigger, "*") {
			if matched, _ := filepath.Match(trigger, changed); matched {
				return true
			}
			if matched, _ := filepath.Match(trigger, filepath.Base(changed)); matched {
				return true
			}
		}
	}
	return false
}

func (e *Executor) executeCommand(cmd Command) error {
	workingDir := cmd.WorkingDir
	if workingDir == "" || workingDir == "." {
		wd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get working directory: %w", err)
		}
		workingDir = wd
	}

	parts := strings.Fields(cmd.Command)
	if len(parts) == 0 {
		return fmt.Errorf("empty command")
	}

	execCmd := exec.Command(parts[0], parts[1:]...)
	execCmd.Dir = workingDir
	execCmd.Stdout = os.Stdout
	execCmd.Stderr = os.Stderr

	return execCmd.Run()
}
