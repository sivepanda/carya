// Package git provides git plumbing operations for Carya's shadow repository system.
package git

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// ShadowRepo manages the shadow git repository stored in .carya/shadow/
// All methods are serialized via an internal mutex to prevent concurrent
// git commands from racing on the index (which causes index.lock errors).
type ShadowRepo struct {
	mu       sync.Mutex
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

func (s *ShadowRepo) Initialize() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, err := os.Stat(s.gitDir); err != nil {
		if err := os.MkdirAll(s.gitDir, 0755); err != nil {
			log.Printf("Failed to create shadow directory: %v", err)
			return fmt.Errorf("failed to create shadow directory: %w", err)
		}

		cmd := exec.Command("git", "init", "--bare")
		cmd.Dir = s.gitDir
		cmd.Env = append(os.Environ(), "GIT_DIR="+s.gitDir)
		if output, err := cmd.CombinedOutput(); err != nil {
			log.Printf("Failed to initialize shadow repo: %v, output: %s", err, output)
			return fmt.Errorf("failed to initialize shadow repo: %w\nOutput: %s", err, output)
		}
	}

	return s.removeStaleAlternates()
}

// gitEnv returns the environment for shadow repo git commands. GIT_DIR keeps
// the shadow index isolated from the main repo, while GIT_OBJECT_DIRECTORY
// routes all object reads and writes to the main repo's object store. Objects
// must live in the main store because the refs that keep them alive
// (refs/carya/*) are main-repo refs: an object stored only in the shadow repo
// is invisible to the main repo's update-ref/push and unprotected from gc.
func (s *ShadowRepo) gitEnv() []string {
	return append(os.Environ(),
		"GIT_DIR="+s.gitDir,
		"GIT_OBJECT_DIRECTORY="+s.mainObjectsDir(),
	)
}

func (s *ShadowRepo) mainObjectsDir() string {
	cmd := exec.Command("git", "rev-parse", "--path-format=absolute", "--git-path", "objects")
	cmd.Dir = s.workTree
	if output, err := cmd.Output(); err == nil {
		return strings.TrimSpace(string(output))
	}
	return filepath.Join(s.workTree, ".git", "objects")
}

// removeStaleAlternates cleans up the bidirectional alternates links that
// older versions wired between the main and shadow object stores. The
// main->shadow link let the main repo's gc prune objects the shadow repo
// still referenced (and vice versa), corrupting whichever store lost the
// race. Neither link is needed now that shadow commands write objects
// directly into the main store.
func (s *ShadowRepo) removeStaleAlternates() error {
	shadowAlt := filepath.Join(s.gitDir, "objects", "info", "alternates")
	if err := os.Remove(shadowAlt); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove shadow alternates: %w", err)
	}

	mainAlt := filepath.Join(s.mainObjectsDir(), "info", "alternates")
	data, err := os.ReadFile(mainAlt)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to read main alternates: %w", err)
	}

	shadowObjects := filepath.Join(s.gitDir, "objects")
	var kept []string
	for _, line := range strings.Split(string(data), "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" && trimmed != shadowObjects {
			kept = append(kept, trimmed)
		}
	}

	if len(kept) == 0 {
		if err := os.Remove(mainAlt); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove main alternates: %w", err)
		}
		return nil
	}
	if err := os.WriteFile(mainAlt, []byte(strings.Join(kept, "\n")+"\n"), 0644); err != nil {
		return fmt.Errorf("failed to rewrite main alternates: %w", err)
	}
	return nil
}

func (s *ShadowRepo) SeedIndexFromMainHEAD() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	cmd := exec.Command("git", "rev-parse", "HEAD^{tree}")
	cmd.Dir = s.workTree
	output, err := cmd.Output()
	if err != nil {
		log.Printf("Failed to get HEAD tree: %v", err)
		return fmt.Errorf("failed to get HEAD tree: %w", err)
	}
	return s.readTree(strings.TrimSpace(string(output)))
}

// GetIndexEntry returns the blob hash for a file in the shadow index,
// or empty string if the file is not tracked.
func (s *ShadowRepo) GetIndexEntry(path string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	cmd := exec.Command("git", "ls-files", "--cached", "-s", "--", path)
	cmd.Env = s.gitEnv()

	output, err := cmd.Output()
	if err != nil {
		return "", nil
	}

	line := strings.TrimSpace(string(output))
	if line == "" {
		return "", nil
	}

	// format: <mode> <hash> <stage>\t<path>
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return "", nil
	}
	return fields[1], nil
}

// GitDir returns the path to the shadow git directory.
func (s *ShadowRepo) GitDir() string {
	return s.gitDir
}

// HashObject stores content as a git blob and returns its hash.
func (s *ShadowRepo) HashObject(content []byte) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	cmd := exec.Command("git", "hash-object", "-w", "--stdin")
	cmd.Env = s.gitEnv()
	cmd.Stdin = bytes.NewReader(content)

	output, err := cmd.Output()
	if err != nil {
		log.Printf("Failed to hash object: %v", err)
		return "", fmt.Errorf("failed to hash object: %w", err)
	}

	return strings.TrimSpace(string(output)), nil
}

// GetObjectContent retrieves the content of a git blob by its hash.
func (s *ShadowRepo) GetObjectContent(hash string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	cmd := exec.Command("git", "cat-file", "-p", hash)
	cmd.Env = s.gitEnv()

	output, err := cmd.Output()
	if err != nil {
		log.Printf("Failed to get object content: %v", err)
		return nil, fmt.Errorf("failed to get object content: %w", err)
	}

	return output, nil
}

// UpdateIndex adds or updates an entry in the git index.
func (s *ShadowRepo) UpdateIndex(path, blobHash, mode string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	cmd := exec.Command("git", "update-index", "--add", "--cacheinfo", fmt.Sprintf("%s,%s,%s", mode, blobHash, path))
	cmd.Env = s.gitEnv()

	if output, err := cmd.CombinedOutput(); err != nil {
		log.Printf("Failed to update index: %v, output: %s", err, output)
		return fmt.Errorf("failed to update index: %w\nOutput: %s", err, output)
	}

	return nil
}

// RemoveFromIndex removes a file from the git index.
func (s *ShadowRepo) RemoveFromIndex(path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	cmd := exec.Command("git", "update-index", "--remove", path)
	cmd.Env = s.gitEnv()

	// Ignore errors if file wasn't in index
	cmd.Run()
	return nil
}

// WriteTree writes the current index as a tree object and returns its hash.
func (s *ShadowRepo) WriteTree() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	cmd := exec.Command("git", "write-tree")
	cmd.Env = s.gitEnv()

	output, err := cmd.Output()
	if err != nil {
		log.Printf("Failed to write tree: %v", err)
		return "", fmt.Errorf("failed to write tree: %w", err)
	}

	return strings.TrimSpace(string(output)), nil
}

// DiffBlobs generates a unified diff between two blobs.
func (s *ShadowRepo) DiffBlobs(oldHash, newHash, path string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// If either hash is empty, handle creation/deletion
	if oldHash == "" {
		oldHash = "/dev/null"
	}
	if newHash == "" {
		newHash = "/dev/null"
	}

	cmd := exec.Command("git", "diff", "--no-color", oldHash, newHash, "--", path)
	cmd.Env = s.gitEnv()

	output, err := cmd.Output()
	if err != nil {
		// git diff returns exit code 1 when there are differences, which is normal
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return string(output), nil
		}
		log.Printf("Failed to diff blobs: %v", err)
		return "", fmt.Errorf("failed to diff blobs: %w", err)
	}

	return string(output), nil
}

// DiffBlobsRaw generates a diff between two blobs using the raw blob hashes.
func (s *ShadowRepo) DiffBlobsRaw(oldHash, newHash string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if oldHash == "" || newHash == "" {
		log.Printf("Both hashes must be provided for DiffBlobsRaw")
		return "", fmt.Errorf("both hashes must be provided")
	}

	cmd := exec.Command("git", "diff", "--no-color", oldHash, newHash)
	cmd.Env = s.gitEnv()

	output, err := cmd.Output()
	if err != nil {
		// git diff returns exit code 1 when there are differences
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return string(output), nil
		}
		log.Printf("Failed to diff blobs (raw): %v", err)
		return "", fmt.Errorf("failed to diff blobs: %w", err)
	}

	return string(output), nil
}

// DiffFilesWithPath generates a patch between two file paths and rewrites headers
// to point at the provided repository-relative path.
func (s *ShadowRepo) DiffFilesWithPath(oldPath, newPath, targetPath string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	cmd := exec.Command("git", "diff", "--no-index", oldPath, newPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); !ok || exitErr.ExitCode() != 1 {
			return "", fmt.Errorf("failed to diff files: %w", err)
		}
	}

	diff := string(output)
	if strings.TrimSpace(diff) == "" {
		return "", nil
	}

	oldLabel := oldPath
	newLabel := newPath
	if oldPath != "/dev/null" {
		oldLabel = strings.TrimPrefix(oldPath, "/")
	}
	if newPath != "/dev/null" {
		newLabel = strings.TrimPrefix(newPath, "/")
	}

	diff = strings.ReplaceAll(diff, "a/"+oldLabel, "a/"+targetPath)
	diff = strings.ReplaceAll(diff, "b/"+newLabel, "b/"+targetPath)
	if oldPath != "/dev/null" {
		diff = strings.ReplaceAll(diff, "b/"+oldLabel, "b/"+targetPath)
	}
	if newPath != "/dev/null" {
		diff = strings.ReplaceAll(diff, "a/"+newLabel, "a/"+targetPath)
	}

	return diff, nil
}

// ObjectExists checks if a git object exists.
func (s *ShadowRepo) ObjectExists(hash string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	cmd := exec.Command("git", "cat-file", "-e", hash)
	cmd.Env = s.gitEnv()
	return cmd.Run() == nil
}

// ReadTree reads a tree object into the index.
func (s *ShadowRepo) ReadTree(treeHash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.readTree(treeHash)
}

func (s *ShadowRepo) readTree(treeHash string) error {
	cmd := exec.Command("git", "read-tree", treeHash)
	cmd.Env = s.gitEnv()

	if output, err := cmd.CombinedOutput(); err != nil {
		log.Printf("Failed to read tree: %v, output: %s", err, output)
		return fmt.Errorf("failed to read tree: %w\nOutput: %s", err, output)
	}

	return nil
}

// CheckoutTree checks out a tree to the working directory.
func (s *ShadowRepo) CheckoutTree(treeHash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// First read the tree into index
	if err := s.readTree(treeHash); err != nil {
		return err
	}

	// Then checkout the index
	cmd := exec.Command("git", "checkout-index", "-a", "-f")
	cmd.Dir = s.workTree
	cmd.Env = append(s.gitEnv(), "GIT_WORK_TREE="+s.workTree)

	if output, err := cmd.CombinedOutput(); err != nil {
		log.Printf("Failed to checkout tree: %v, output: %s", err, output)
		return fmt.Errorf("failed to checkout tree: %w\nOutput: %s", err, output)
	}

	return nil
}

// ListTree lists the contents of a tree object.
func (s *ShadowRepo) ListTree(treeHash string) ([]TreeEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	cmd := exec.Command("git", "ls-tree", "-r", treeHash)
	cmd.Env = s.gitEnv()

	output, err := cmd.Output()
	if err != nil {
		log.Printf("Failed to list tree: %v", err)
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
