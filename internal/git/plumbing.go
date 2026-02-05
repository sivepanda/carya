// Package git provides git plumbing operations for Carya's shadow repository system.
package git

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ShadowRepo manages the shadow git repository stored in .carya/shadow/
type ShadowRepo struct {
	gitDir   string // Path to .carya/shadow/
	workTree string // Path to repo root
}

// NewShadowRepo creates a new ShadowRepo instance.
func NewShadowRepo(caryaPath, workTree string) *ShadowRepo {
	return &ShadowRepo{
		gitDir:   filepath.Join(caryaPath, "shadow"),
		workTree: workTree,
	}
}

// Initialize creates the shadow git repository if it doesn't exist.
func (s *ShadowRepo) Initialize() error {
	if _, err := os.Stat(s.gitDir); err == nil {
		// Already exists
		return nil
	}

	// Create the shadow directory
	if err := os.MkdirAll(s.gitDir, 0755); err != nil {
		return fmt.Errorf("failed to create shadow directory: %w", err)
	}

	// Initialize bare-like git repo
	cmd := exec.Command("git", "init", "--bare")
	cmd.Dir = s.gitDir
	cmd.Env = append(os.Environ(), "GIT_DIR="+s.gitDir)

	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to initialize shadow repo: %w\nOutput: %s", err, output)
	}

	return nil
}

// GitDir returns the path to the shadow git directory.
func (s *ShadowRepo) GitDir() string {
	return s.gitDir
}

// HashObject stores content as a git blob and returns its hash.
func (s *ShadowRepo) HashObject(content []byte) (string, error) {
	cmd := exec.Command("git", "hash-object", "-w", "--stdin")
	cmd.Env = append(os.Environ(), "GIT_DIR="+s.gitDir)
	cmd.Stdin = bytes.NewReader(content)

	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to hash object: %w", err)
	}

	return strings.TrimSpace(string(output)), nil
}

// GetObjectContent retrieves the content of a git blob by its hash.
func (s *ShadowRepo) GetObjectContent(hash string) ([]byte, error) {
	cmd := exec.Command("git", "cat-file", "-p", hash)
	cmd.Env = append(os.Environ(), "GIT_DIR="+s.gitDir)

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get object content: %w", err)
	}

	return output, nil
}

// UpdateIndex adds or updates an entry in the git index.
func (s *ShadowRepo) UpdateIndex(path, blobHash, mode string) error {
	cmd := exec.Command("git", "update-index", "--add", "--cacheinfo", fmt.Sprintf("%s,%s,%s", mode, blobHash, path))
	cmd.Env = append(os.Environ(), "GIT_DIR="+s.gitDir)

	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to update index: %w\nOutput: %s", err, output)
	}

	return nil
}

// RemoveFromIndex removes a file from the git index.
func (s *ShadowRepo) RemoveFromIndex(path string) error {
	cmd := exec.Command("git", "update-index", "--remove", path)
	cmd.Env = append(os.Environ(), "GIT_DIR="+s.gitDir)

	// Ignore errors if file wasn't in index
	cmd.Run()
	return nil
}

// WriteTree writes the current index as a tree object and returns its hash.
func (s *ShadowRepo) WriteTree() (string, error) {
	cmd := exec.Command("git", "write-tree")
	cmd.Env = append(os.Environ(), "GIT_DIR="+s.gitDir)

	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to write tree: %w", err)
	}

	return strings.TrimSpace(string(output)), nil
}

// DiffBlobs generates a unified diff between two blobs.
func (s *ShadowRepo) DiffBlobs(oldHash, newHash, path string) (string, error) {
	// If either hash is empty, handle creation/deletion
	if oldHash == "" {
		oldHash = "/dev/null"
	}
	if newHash == "" {
		newHash = "/dev/null"
	}

	cmd := exec.Command("git", "diff", "--no-color", oldHash, newHash, "--", path)
	cmd.Env = append(os.Environ(), "GIT_DIR="+s.gitDir)

	output, err := cmd.Output()
	if err != nil {
		// git diff returns exit code 1 when there are differences, which is normal
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return string(output), nil
		}
		return "", fmt.Errorf("failed to diff blobs: %w", err)
	}

	return string(output), nil
}

// DiffBlobsRaw generates a diff between two blobs using the raw blob hashes.
func (s *ShadowRepo) DiffBlobsRaw(oldHash, newHash string) (string, error) {
	if oldHash == "" || newHash == "" {
		return "", fmt.Errorf("both hashes must be provided")
	}

	cmd := exec.Command("git", "diff", "--no-color", oldHash, newHash)
	cmd.Env = append(os.Environ(), "GIT_DIR="+s.gitDir)

	output, err := cmd.Output()
	if err != nil {
		// git diff returns exit code 1 when there are differences
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return string(output), nil
		}
		return "", fmt.Errorf("failed to diff blobs: %w", err)
	}

	return string(output), nil
}

// ObjectExists checks if a git object exists.
func (s *ShadowRepo) ObjectExists(hash string) bool {
	cmd := exec.Command("git", "cat-file", "-e", hash)
	cmd.Env = append(os.Environ(), "GIT_DIR="+s.gitDir)
	return cmd.Run() == nil
}

// ReadTree reads a tree object into the index.
func (s *ShadowRepo) ReadTree(treeHash string) error {
	cmd := exec.Command("git", "read-tree", treeHash)
	cmd.Env = append(os.Environ(), "GIT_DIR="+s.gitDir)

	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to read tree: %w\nOutput: %s", err, output)
	}

	return nil
}

// CheckoutTree checks out a tree to the working directory.
func (s *ShadowRepo) CheckoutTree(treeHash string) error {
	// First read the tree into index
	if err := s.ReadTree(treeHash); err != nil {
		return err
	}

	// Then checkout the index
	cmd := exec.Command("git", "checkout-index", "-a", "-f")
	cmd.Dir = s.workTree
	cmd.Env = append(os.Environ(), "GIT_DIR="+s.gitDir, "GIT_WORK_TREE="+s.workTree)

	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to checkout tree: %w\nOutput: %s", err, output)
	}

	return nil
}

// ListTree lists the contents of a tree object.
func (s *ShadowRepo) ListTree(treeHash string) ([]TreeEntry, error) {
	cmd := exec.Command("git", "ls-tree", "-r", treeHash)
	cmd.Env = append(os.Environ(), "GIT_DIR="+s.gitDir)

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to list tree: %w", err)
	}

	var entries []TreeEntry
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		// Format: <mode> <type> <hash>\t<path>
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			continue
		}
		metaParts := strings.Fields(parts[0])
		if len(metaParts) != 3 {
			continue
		}
		entries = append(entries, TreeEntry{
			Mode: metaParts[0],
			Type: metaParts[1],
			Hash: metaParts[2],
			Path: parts[1],
		})
	}

	return entries, nil
}

// TreeEntry represents an entry in a git tree.
type TreeEntry struct {
	Mode string
	Type string
	Hash string
	Path string
}
