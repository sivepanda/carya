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
		fsWatcher: fsWatcher,
		handler:   handler,
		stopCh:    make(chan struct{}),
	}, nil
}

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

func (w *Watcher) Stop() {
	close(w.stopCh)
	if w.fsWatcher != nil {
		w.fsWatcher.Close()
	}
}

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
			log.Print("test1")
			//test
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

func (w *Watcher) shouldTrackFile(path string) bool {
	if w.shouldIgnore(path, false) {
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
