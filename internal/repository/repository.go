package repository

import (
	"fmt"
	"os"
	"path/filepath"
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
