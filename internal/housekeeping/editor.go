package housekeeping

import (
	"fmt"
	"os"
	"os/exec"
)

func OpenConfigInEditor() error {
	configPath, err := GetConfigPath()
	if err != nil {
		return err
	}

	// Ensure config file exists
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		config := NewConfig()
		if err := config.Save(); err != nil {
			return fmt.Errorf("failed to create initial config file: %w", err)
		}
	}

	// Get editor from environment
	editor := os.Getenv("EDITOR")
	if editor == "" {
		// Try common editors as fallbacks
		editors := []string{"nano", "vim", "vi", "code", "subl"}
		for _, e := range editors {
			if _, err := exec.LookPath(e); err == nil {
				editor = e
				break
			}
		}
	}

	if editor == "" {
		return fmt.Errorf("no editor found. Set the EDITOR environment variable or install nano, vim, or code")
	}

	fmt.Printf("Opening %s in %s...\n", configPath, editor)

	cmd := exec.Command(editor, configPath)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}
