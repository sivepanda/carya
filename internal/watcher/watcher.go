package watcher

import (
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
	fsWatcher *fsnotify.Watcher
	handler   FileChangeHandler
	stopCh    chan struct{}
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

func (w *Watcher) handleEvent(event fsnotify.Event) {
	// Handle file modifications and writes
	if event.Op&fsnotify.Write == fsnotify.Write || event.Op&fsnotify.Create == fsnotify.Create {
		fi, err := os.Stat(event.Name)
		if err != nil {
			return
		}

		// If it's a new directory, add it to the watcher
		if fi.IsDir() && event.Op&fsnotify.Create == fsnotify.Create {
			if err := w.fsWatcher.Add(event.Name); err != nil {
				log.Println("ERROR adding directory to watcher:", err)
			} else {
				log.Println("Added to watch:", event.Name)
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
}

func (w *Watcher) shouldSkipDir(path string) bool {
	skipDirs := []string{".git", "node_modules", ".vscode", ".idea", "target", "build", "dist"}
	for _, skip := range skipDirs {
		if strings.Contains(path, skip) {
			return true
		}
	}
	return false
}

func (w *Watcher) shouldTrackFile(path string) bool {
	// Track common source code files
	ext := strings.ToLower(filepath.Ext(path))
	trackableExts := []string{
		".go", ".js", ".ts", ".jsx", ".tsx", ".py", ".java", ".c", ".cpp", ".h", ".hpp",
		".rs", ".rb", ".php", ".cs", ".swift", ".kt", ".scala", ".clj", ".hs", ".ml",
		".sh", ".bash", ".zsh", ".fish", ".ps1", ".bat", ".cmd",
		".html", ".css", ".scss", ".sass", ".less", ".vue", ".svelte",
		".json", ".yaml", ".yml", ".toml", ".xml", ".md", ".txt", ".sql",
	}

	for _, trackable := range trackableExts {
		if ext == trackable {
			return true
		}
	}
	return false
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
