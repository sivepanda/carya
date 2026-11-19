package repository

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"os/exec"
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
	return NewAt(wd)
}

// NewAt creates a repository instance for the checkout containing dir. In a
// linked worktree that is the worktree, which has its own .carya.
func NewAt(dir string) (*Repository, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	cmd.Dir = dir
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("not inside a git repository (run 'git init' first): %w", err)
	}
	rootPath := strings.TrimSpace(string(output))

	return &Repository{
		rootPath:  rootPath,
		caryaPath: filepath.Join(rootPath, ".carya"),
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

// EnsureLocallyIgnored keeps .carya/ out of git without touching any tracked
// file: if nothing ignores it yet, it is added to the repository's
// info/exclude, which every worktree of the repository shares.
func (r *Repository) EnsureLocallyIgnored() error {
	check := exec.Command("git", "check-ignore", "-q", "--no-index", ".carya/user-id")
	check.Dir = r.rootPath
	if check.Run() == nil {
		return nil
	}

	cmd := exec.Command("git", "rev-parse", "--git-common-dir")
	cmd.Dir = r.rootPath
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("locate git dir: %w", err)
	}
	commonDir := strings.TrimSpace(string(out))
	if !filepath.IsAbs(commonDir) {
		commonDir = filepath.Join(r.rootPath, commonDir)
	}
	excludePath := filepath.Join(commonDir, "info", "exclude")
	if err := os.MkdirAll(filepath.Dir(excludePath), 0755); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(excludePath), err)
	}

	existing, _ := os.ReadFile(excludePath)
	entry := ".carya/\n"
	if len(existing) > 0 && !strings.HasSuffix(string(existing), "\n") {
		entry = "\n" + entry
	}
	f, err := os.OpenFile(excludePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open %s: %w", excludePath, err)
	}
	defer f.Close()
	if _, err := f.WriteString(entry); err != nil {
		return fmt.Errorf("write %s: %w", excludePath, err)
	}
	return nil
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
