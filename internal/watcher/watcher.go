// Package watcher provides file system monitoring capabilities for the Carya
// version control system, tracking file changes and respecting gitignore rules.
package watcher

import (
	"bufio"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/fsnotify/fsnotify"
)

// FileChangeHandler defines the interface for handling file change events.
type FileChangeHandler interface {
	// OnFileChange is called when a tracked file is modified. A nil
	// contents slice means the file was removed.
	OnFileChange(path string, contents []byte)
}

// TrackedPathLister is optionally implemented by handlers that can list the
// files they track under a directory. When a watched directory is removed or
// renamed away its contents can no longer be listed from disk, so the watcher
// asks the handler which files to report as deleted.
type TrackedPathLister interface {
	TrackedPaths(dir string) []string
}

// Watcher monitors file system changes in a directory tree, respecting gitignore rules
// and filtering out binary files and unwanted directories.
type Watcher struct {
	fsWatcher      *fsnotify.Watcher // Underlying file system watcher
	handler        FileChangeHandler // Handler for file change events
	stopCh         chan struct{}     // Channel to signal shutdown
	gitignoreRules []string          // Rules for ignoring files/directories
	watchDir       string            // Root directory being watched

	mu   sync.Mutex      // Guards dirs (the initial walk and the event loop both add)
	dirs map[string]bool // Directories currently watched (absolute paths)
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
		dirs:      make(map[string]bool),
	}, nil
}

// LoadRules sets the watched root and loads its ignore rules without starting
// to watch, so callers can apply the same filters (see ShouldTrackRel) before
// Start. Start calls it as well.
func (w *Watcher) LoadRules(watchDir string) {
	w.watchDir = watchDir
	w.loadGitignoreRules()
}

// ShouldTrackRel reports whether the watcher would track the file at relPath
// (relative to the watched root). LoadRules or Start must have been called.
func (w *Watcher) ShouldTrackRel(relPath string) bool {
	return w.shouldTrackFile(filepath.Join(w.watchDir, relPath))
}

// Start begins watching the specified directory tree for file changes.
// It loads gitignore rules and recursively adds directories to the watch list.
func (w *Watcher) Start(watchDir string) error {
	w.LoadRules(watchDir)

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
			if err := w.addWatch(path); err != nil {
				return err
			}
			log.Println("Watching:", path)
		}
		return nil
	})
}

func (w *Watcher) addWatch(dir string) error {
	if err := w.fsWatcher.Add(dir); err != nil {
		return err
	}
	w.mu.Lock()
	w.dirs[dir] = true
	w.mu.Unlock()
	return nil
}

// forgetDir drops path and every watched directory below it, reporting
// whether path itself was a watched directory.
func (w *Watcher) forgetDir(path string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Most removals are files; skip the scan for them. A path not in the map
	// never had watched subdirectories (addTree skips a subtree whose watch
	// fails, and a failure in the initial walk aborts Start).
	if !w.dirs[path] {
		return false
	}
	prefix := path + string(filepath.Separator)
	for dir := range w.dirs {
		if dir == path || strings.HasPrefix(dir, prefix) {
			delete(w.dirs, dir)
			// A renamed-away directory keeps its inotify watch under the old
			// path; drop it. Removing an already-gone watch just errors.
			_ = w.fsWatcher.Remove(dir)
		}
	}
	return true
}

// addTree watches a newly created directory and everything below it, and
// reports files already inside it. Files written before the watch on their
// directory was registered (`mkdir -p a/b && echo x > a/b/f`, a branch
// checkout, an unpacked archive) produce no events of their own.
func (w *Watcher) addTree(root string) {
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// Vanished mid-walk; whatever replaced it raises its own events.
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if w.shouldIgnore(path, true) {
				return filepath.SkipDir
			}
			if err := w.addWatch(path); err != nil {
				log.Printf("Failed to watch %s: %v", path, err)
				return filepath.SkipDir
			}
			log.Println("Added to watch:", path)
			return nil
		}
		if d.Type().IsRegular() && w.shouldTrackFile(path) {
			w.reportFile(path)
		}
		return nil
	})
}

func (w *Watcher) relPath(path string) string {
	rel, err := filepath.Rel(w.watchDir, path)
	if err != nil {
		log.Printf("Failed to get relative path for %s: %v", path, err)
		return path
	}
	return rel
}

// reportFile reads a tracked file and passes its contents to the handler.
func (w *Watcher) reportFile(path string) {
	if w.handler == nil {
		return
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return
	}
	w.handler.OnFileChange(w.relPath(path), contents)
}

// handleRemoval reports a removed or renamed-away path as deleted. A
// directory is expanded into the tracked files that were under it.
func (w *Watcher) handleRemoval(path string) {
	if _, err := os.Lstat(path); err == nil {
		// Recreated already (e.g. an editor's rename-then-write save); the
		// Create/Write event for the new file reports its contents.
		return
	}

	rel := w.relPath(path)
	paths := []string{rel}
	if w.forgetDir(path) {
		paths = nil
		if lister, ok := w.handler.(TrackedPathLister); ok {
			paths = lister.TrackedPaths(rel)
		}
	}

	if w.handler == nil {
		return
	}
	for _, p := range paths {
		if w.shouldTrackFile(filepath.Join(w.watchDir, p)) {
			w.handler.OnFileChange(p, nil)
		}
	}
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
	w.gitignoreRules = []string{".git/", ".carya/", "node_modules/", ".vscode/", ".idea/"}

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
	if relPath == ".carya" || strings.HasPrefix(relPath, ".carya"+string(filepath.Separator)) {
		return true
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
		fi, err := os.Stat(event.Name)
		if err != nil {
			return
		}

		if fi.IsDir() && event.Op&fsnotify.Create == fsnotify.Create {
			if !w.shouldIgnore(event.Name, true) {
				w.addTree(event.Name)
			}
			return
		}

		if !fi.IsDir() && w.shouldTrackFile(event.Name) {
			w.reportFile(event.Name)
		}
	}

	// A rename reports the old name; the new name arrives as a Create.
	if event.Op&(fsnotify.Remove|fsnotify.Rename) != 0 {
		w.handleRemoval(event.Name)
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
