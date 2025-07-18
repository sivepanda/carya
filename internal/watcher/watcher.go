package watcher

import (
	"bufio"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/fsnotify/fsnotify"
)

type FileChangeHandler interface {
	OnFileChange(path string, contents []byte)
}

type Watcher struct {
	fsWatcher      *fsnotify.Watcher
	handler        FileChangeHandler
	stopCh         chan struct{}
	gitignoreRules []string
	watchDir       string
}

func New(handler FileChangeHandler) (*Watcher, error) {
	fsWatcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	return &Watcher{
		fsWatcher:      fsWatcher,
		handler:        handler,
		stopCh:         make(chan struct{}),
		gitignoreRules: []string{},
	}, nil
}

func (w *Watcher) Start(watchDir string) error {
	w.watchDir = watchDir
	w.loadGitignoreRules()

	// Start listening for events.
	go func() {
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
	}()

	// Walk the directory tree and add each directory to the watcher.
	log.Println("Walking directory:", watchDir)
	return filepath.Walk(watchDir, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip .git directories and other common ignore patterns
		if fi.IsDir() && w.shouldSkipDir(path) {
			return filepath.SkipDir
		}

		if fi.IsDir() {
			if err := w.fsWatcher.Add(path); err != nil {
				return err
			}
			log.Println("Watching:", path)
		}
		return nil
	})
}

func (w *Watcher) Stop() {
	close(w.stopCh)
	if w.fsWatcher != nil {
		w.fsWatcher.Close()
	}
}

func (w *Watcher) loadGitignoreRules() {
	// Always add .git to ignore rules
	w.gitignoreRules = []string{".git/"}

	gitignorePath := filepath.Join(w.watchDir, ".gitignore")
	file, err := os.Open(gitignorePath)
	if err != nil {
		log.Printf("No .gitignore found at %s, using default rules", gitignorePath)
		w.gitignoreRules = append(w.gitignoreRules, "node_modules/", ".vscode/", ".idea/")
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

	if err := scanner.Err(); err != nil {
		log.Printf("Error reading .gitignore: %v", err)
	}
}
func (w *Watcher) matchesGitignore(path string, isDir bool) bool {
	for _, rule := range w.gitignoreRules {
		if w.matchesRule(path, rule, isDir) {
			return true
		}
	}
	return false
}

func (w *Watcher) matchesRule(path, rule string, isDir bool) bool {
	// Handle directory-specific rules (ending with /)
	if strings.HasSuffix(rule, "/") {
		if !isDir {
			return false
		}
		rule = strings.TrimSuffix(rule, "/")
	}

	// Handle glob patterns
	if strings.Contains(rule, "*") {
		matched, _ := filepath.Match(rule, path)
		if matched {
			return true
		}
		// Also check if any parent directory matches
		dir := filepath.Dir(path)
		for dir != "." && dir != "/" {
			matched, _ := filepath.Match(rule, filepath.Base(dir))
			if matched {
				return true
			}
			dir = filepath.Dir(dir)
		}
		return false
	}

	// Exact match or prefix match
	if path == rule || strings.HasPrefix(path, rule+"/") {
		return true
	}

	// Check if any parent directory matches
	parts := strings.Split(path, "/")
	for i := range parts {
		if parts[i] == rule {
			return true
		}
	}

	return false
}

func (w *Watcher) handleEvent(event fsnotify.Event) {
	// Handle file modifications and writes
	if event.Op&fsnotify.Write == fsnotify.Write || event.Op&fsnotify.Create == fsnotify.Create {
		fi, err := os.Stat(event.Name)
		if err != nil {
			return
		}

		// If it's a new directory, add it to the watcher (unless it should be skipped)
		if fi.IsDir() && event.Op&fsnotify.Create == fsnotify.Create {
			if !w.shouldSkipDir(event.Name) {
				if err := w.fsWatcher.Add(event.Name); err != nil {
					log.Println("ERROR adding directory to watcher:", err)
				} else {
					log.Println("Added to watch:", event.Name)
				}
			}
			return
		}

		// If it's a file and we should track it, read contents and notify handler
		if !fi.IsDir() && w.shouldTrackFile(event.Name) {
			contents, err := os.ReadFile(event.Name)
			if err != nil {
				log.Printf("Error reading file %s: %v", event.Name, err)
				return
			}

			if w.handler != nil {
				w.handler.OnFileChange(event.Name, contents)
			}
		}
	}

	// Handle directory/file removal
	if event.Op&fsnotify.Remove == fsnotify.Remove {
		if err := w.fsWatcher.Remove(event.Name); err != nil {
			log.Printf("Note: Could not remove %s from watcher: %v", event.Name, err)
		} else {
			log.Println("Removed from watch:", event.Name)
		}
	}
}

func (w *Watcher) shouldSkipDir(path string) bool {
	relPath, err := filepath.Rel(w.watchDir, path)
	if err != nil {
		return false
	}

	return w.matchesGitignore(relPath, true)
}

func (w *Watcher) shouldTrackFile(path string) bool {
	relPath, err := filepath.Rel(w.watchDir, path)
	if err != nil {
		return false
	}

	// If file matches gitignore patterns, don't track it
	if w.matchesGitignore(relPath, false) {
		return false
	}

	// Track common source code files (exclusion-based approach)
	ext := strings.ToLower(filepath.Ext(path))

	// Skip binary and generated files
	binaryExts := []string{
		".exe", ".dll", ".so", ".dylib", ".bin", ".out", ".o", ".a",
		".jpg", ".jpeg", ".png", ".gif", ".bmp", ".ico", ".svg",
		".mp3", ".mp4", ".avi", ".mov", ".wav", ".pdf", ".zip", ".tar", ".gz",
	}

	for _, binary := range binaryExts {
		if ext == binary {
			return false
		}
	}

	// Track text-based files by default (unless explicitly ignored)
	return true
}

// Legacy function for backward compatibility
func Start(watchDir string) {
	watcher, err := New(nil)
	if err != nil {
		log.Fatal("ERROR creating watcher:", err)
	}
	defer watcher.Stop()

	if err := watcher.Start(watchDir); err != nil {
		log.Fatal("ERROR starting watcher:", err)
	}

	// Block main goroutine forever.
	<-make(chan struct{})
}
