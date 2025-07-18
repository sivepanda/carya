package housekeeping

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

type Executor struct {
	config *Config
}

func NewExecutor(config *Config) *Executor {
	return &Executor{config: config}
}

func (e *Executor) ExecuteCategory(category string, autoApprove bool) error {
	commands, err := e.config.GetCommands(category)
	if err != nil {
		return err
	}

	if len(commands) == 0 {
		fmt.Printf("No %s commands configured.\n", category)
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
		reader := bufio.NewReader(os.Stdin)
		response, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("failed to read user input: %w", err)
		}

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
