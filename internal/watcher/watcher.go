// Package watcher provides file system monitoring capabilities for the Carya
// version control system, tracking file changes and respecting gitignore rules.
package watcher

import (
	"bufio"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/fsnotify/fsnotify"
)

// FileChangeHandler defines the interface for handling file change events.
type FileChangeHandler interface {
	// OnFileChange is called when a tracked file is modified.
	OnFileChange(path string, contents []byte)
}

// Watcher monitors file system changes in a directory tree, respecting gitignore rules
// and filtering out binary files and unwanted directories.
type Watcher struct {
	fsWatcher      *fsnotify.Watcher // Underlying file system watcher
	handler        FileChangeHandler // Handler for file change events
	stopCh         chan struct{}     // Channel to signal shutdown
	gitignoreRules []string          // Rules for ignoring files/directories
	watchDir       string            // Root directory being watched
}

// New creates a new file system watcher with the specified change handler.
func New(handler FileChangeHandler) (*Watcher, error) {
	fsWatcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	return &Watcher{
		fsWatcher: fsWatcher,
		handler:   handler,
		stopCh:    make(chan struct{}),
	}, nil
}

// Start begins watching the specified directory tree for file changes.
// It loads gitignore rules and recursively adds directories to the watch list.
func (w *Watcher) Start(watchDir string) error {
	w.watchDir = watchDir
	w.loadGitignoreRules()

	go w.watchLoop()

	log.Println("Walking directory:", watchDir)
	return filepath.Walk(watchDir, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if fi.IsDir() {
			if w.shouldIgnore(path, true) {
				return filepath.SkipDir
			}
			if err := w.fsWatcher.Add(path); err != nil {
				return err
			}
			log.Println("Watching:", path)
		}
		return nil
	})
}

// Stop gracefully shuts down the watcher and closes all resources.
func (w *Watcher) Stop() {
	close(w.stopCh)
	if w.fsWatcher != nil {
		w.fsWatcher.Close()
	}
}

// watchLoop runs in a separate goroutine and processes file system events.
func (w *Watcher) watchLoop() {
	for {
		select {
		case event, ok := <-w.fsWatcher.Events:
			if !ok {
				return
			}
			w.handleEvent(event)

		case err, ok := <-w.fsWatcher.Errors:
			if !ok {
				return
			}
			log.Println("Watcher ERROR:", err)

		case <-w.stopCh:
			return
		}
	}
}

// loadGitignoreRules loads ignore rules from .gitignore file and adds default rules.
func (w *Watcher) loadGitignoreRules() {
	// Default ignore rules
	w.gitignoreRules = []string{".git/", "node_modules/", ".vscode/", ".idea/"}

	gitignorePath := filepath.Join(w.watchDir, ".gitignore")
	file, err := os.Open(gitignorePath)
	if err != nil {
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" && !strings.HasPrefix(line, "#") {
			w.gitignoreRules = append(w.gitignoreRules, line)
		}
	}
}

// shouldIgnore determines if a path should be ignored based on gitignore rules.
func (w *Watcher) shouldIgnore(path string, isDir bool) bool {
	relPath, err := filepath.Rel(w.watchDir, path)
	if err != nil {
		return false
	}

	for _, rule := range w.gitignoreRules {
		if w.matchesRule(relPath, rule, isDir) {
			return true
		}
	}
	return false
}

// matchesRule checks if a path matches a specific gitignore rule.
func (w *Watcher) matchesRule(path, rule string, isDir bool) bool {
	// Directory-specific rules
	if strings.HasSuffix(rule, "/") {
		if !isDir {
			return false
		}
		rule = strings.TrimSuffix(rule, "/")
	}

	// Simple glob or exact match
	if matched, _ := filepath.Match(rule, path); matched {
		return true
	}

	// Check if any part of the path matches
	parts := strings.Split(path, "/")
	return slices.Contains(parts, rule)
}

// handleEvent processes a file system event and triggers appropriate actions.
func (w *Watcher) handleEvent(event fsnotify.Event) {
	if event.Op&(fsnotify.Write|fsnotify.Create) != 0 {
		log.Print("test")
		fi, err := os.Stat(event.Name)
		if err != nil {
			return
		}

		if fi.IsDir() && event.Op&fsnotify.Create == fsnotify.Create {
			if !w.shouldIgnore(event.Name, true) {
				w.fsWatcher.Add(event.Name)
				log.Println("Added to watch:", event.Name)
			}
			return
		}

		if !fi.IsDir() && w.shouldTrackFile(event.Name) {
			contents, err := os.ReadFile(event.Name)
			if err != nil {
				return
			}
			if w.handler != nil {
				w.handler.OnFileChange(event.Name, contents)
			}
		}
	}

	if event.Op&fsnotify.Remove == fsnotify.Remove {
		w.fsWatcher.Remove(event.Name)
	}
}

// shouldTrackFile determines if a file should be tracked based on ignore rules and file type.
// It excludes binary files, temporary files, and files matching gitignore patterns.
func (w *Watcher) shouldTrackFile(path string) bool {
	if w.shouldIgnore(path, false) {
		return false
	}

	basename := filepath.Base(path)

	// Skip temporary files created by editors
	if strings.Contains(basename, ".tmp") ||
		strings.HasSuffix(basename, "~") ||
		strings.HasSuffix(basename, ".swp") ||
		strings.HasSuffix(basename, ".swo") ||
		strings.HasPrefix(basename, ".#") {
		return false
	}

	// Skip binary files
	ext := strings.ToLower(filepath.Ext(path))
	binaryExts := []string{
		".exe", ".dll", ".so", ".bin", ".out", ".o", ".a",
		".jpg", ".jpeg", ".png", ".gif", ".pdf", ".zip", ".tar", ".gz",
	}

	return !slices.Contains(binaryExts, ext)
}
