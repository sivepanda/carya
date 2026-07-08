package repository

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// Repository represents a Carya repository
type Repository struct {
	rootPath  string
	caryaPath string
}

// New creates a new repository instance for the current working directory
func New() (*Repository, error) {
	wd, err := os.Getwd()
	if err != nil {
		log.Printf("Failed to get working directory: %v", err)
		return nil, fmt.Errorf("failed to get working directory: %w", err)
	}

	return &Repository{
		rootPath:  wd,
		caryaPath: filepath.Join(wd, ".carya"),
	}, nil
}

// EnsureExists creates the .carya directory if it doesn't exist
func (r *Repository) EnsureExists() error {
	if err := os.MkdirAll(r.caryaPath, 0755); err != nil {
		log.Printf("Failed to create .carya directory: %v", err)
		return fmt.Errorf("failed to create .carya directory: %w", err)
	}
	return nil
}

// CaryaPath returns the path to the .carya directory
func (r *Repository) CaryaPath() string {
	return r.caryaPath
}

// RootPath returns the root path of the repository
func (r *Repository) RootPath() string {
	return r.rootPath
}

// DBPath returns the path to the chunks database
func (r *Repository) DBPath() string {
	return filepath.Join(r.caryaPath, "chunks.db")
}

// Exists checks if the .carya directory exists
func (r *Repository) Exists() bool {
	_, err := os.Stat(r.caryaPath)
	return !os.IsNotExist(err)
}

// PIDPath returns the path to the daemon PID file
func (r *Repository) PIDPath() string {
	return filepath.Join(r.caryaPath, "carya.pid")
}

// LogPath returns the path to the daemon log file
func (r *Repository) LogPath() string {
	return filepath.Join(r.caryaPath, "carya.log")
}

// StatusPath returns the path to the daemon status file
func (r *Repository) StatusPath() string {
	return filepath.Join(r.caryaPath, "status.json")
}

// ShadowPath returns the path to the shadow git repository
func (r *Repository) ShadowPath() string {
	return filepath.Join(r.caryaPath, "shadow")
}

// UserIDPath returns the path to the user identity file
func (r *Repository) UserIDPath() string {
	return filepath.Join(r.caryaPath, "user-id")
}

// EnsureGitignore ensures .carya/ is listed in the .gitignore file.
func (r *Repository) EnsureGitignore() error {
	gitignorePath := filepath.Join(r.rootPath, ".gitignore")
	caryaEntry := ".carya/"

	content := ""
	if data, err := os.ReadFile(gitignorePath); err == nil {
		content = string(data)

		scanner := bufio.NewScanner(strings.NewReader(content))
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == caryaEntry || line == ".carya" {
				return nil
			}
		}
	}

	f, err := os.OpenFile(gitignorePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("Failed to open .gitignore: %v", err)
		return fmt.Errorf("failed to open .gitignore: %w", err)
	}
	defer f.Close()

	if len(content) > 0 && !strings.HasSuffix(content, "\n") {
		if _, err := f.WriteString("\n"); err != nil {
			log.Printf("Failed to write to .gitignore: %v", err)
			return fmt.Errorf("failed to write to .gitignore: %w", err)
		}
	}

	if len(content) == 0 {
		if _, err := f.WriteString("# Carya directory\n"); err != nil {
			log.Printf("Failed to write to .gitignore: %v", err)
			return fmt.Errorf("failed to write to .gitignore: %w", err)
		}
	}

	if _, err := f.WriteString(caryaEntry + "\n"); err != nil {
		log.Printf("Failed to write to .gitignore: %v", err)
		return fmt.Errorf("failed to write to .gitignore: %w", err)
	}

	return nil
}
