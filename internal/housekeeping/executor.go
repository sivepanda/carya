package housekeeping

import (
	"bufio"
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

	// Get autodetected commands and filter based on changed files
	detector := NewDetector(".")
	autoCommands, err := detector.GetSuggestedCommands(category)
	if err == nil && len(changedFiles) > 0 {
		// Filter autodetected commands based on changed files
		autoCommands = e.filterCommandsByChangedFiles(autoCommands, changedFiles)
	}

	// Combine configured commands with filtered autodetected commands
	allCommands := append(commands, autoCommands...)

	if len(allCommands) == 0 {
		fmt.Printf("No %s commands configured.\n", category)
		return nil
	}

	fmt.Printf("Found %d %s tasks:\n", len(allCommands), category)
	for _, cmd := range allCommands {
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
	for i, cmd := range allCommands {
		fmt.Printf("[%d/%d] %s\n", i+1, len(allCommands), cmd.Description)
		if err := e.executeCommand(cmd); err != nil {
			return fmt.Errorf("failed to execute command '%s': %w", cmd.Command, err)
		}
	}

	fmt.Println("All housekeeping tasks completed successfully!")
	return nil
}

// filterCommandsByChangedFiles filters commands to only include those whose associated files changed
func (e *Executor) filterCommandsByChangedFiles(commands []Command, changedFiles []string) []Command {
	if len(changedFiles) == 0 {
		// If no changed files list provided, run all commands
		return commands
	}

	// Build a map of package detect files to check
	detectFileMap := make(map[string]bool)
	for _, pkgType := range PackageTypes {
		detectFileMap[pkgType.DetectFile] = true
	}

	// Check which detect files are in the changed files list
	changedDetectFiles := make(map[string]bool)
	for _, changedFile := range changedFiles {
		// Check exact match or pattern match
		for detectFile := range detectFileMap {
			if matchesDetectFile(changedFile, detectFile) {
				changedDetectFiles[detectFile] = true
			}
		}
	}

	// Filter commands based on changed detect files
	var filtered []Command
	for _, cmd := range commands {
		// Find which package type this command belongs to
		for _, pkgType := range PackageTypes {
			for _, categoryCommands := range pkgType.Commands {
				for _, pkgCmd := range categoryCommands {
					if pkgCmd.Command == cmd.Command && changedDetectFiles[pkgType.DetectFile] {
						filtered = append(filtered, cmd)
						goto nextCommand
					}
				}
			}
		}
	nextCommand:
	}

	return filtered
}

// matchesDetectFile checks if a file path matches a detect file pattern
func matchesDetectFile(filePath, detectFile string) bool {
	// Exact match
	if filePath == detectFile {
		return true
	}

	// Check if it's a glob pattern
	if strings.Contains(detectFile, "*") {
		matched, _ := filepath.Match(detectFile, filepath.Base(filePath))
		return matched
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
