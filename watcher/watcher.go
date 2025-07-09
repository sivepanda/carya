package watcher

import (
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/fsnotify/fsnotify"
)

// Start starts the recursive file watcher.
func Start(watchDir string) {
	// Create a new watcher
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatal("ERROR", err)
	}
	defer watcher.Close()

	// Start listening for events.
	go func() {
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				log.Println("EVENT:", event)

				// If a new directory is created, add it to the watcher.
				if event.Op&fsnotify.Create == fsnotify.Create {
					fi, err := os.Stat(event.Name)
					if err == nil && fi.IsDir() {
						if err := watcher.Add(event.Name); err != nil {
							log.Println("ERROR", err)
						}
						log.Println("Added to watch:", event.Name)
					}
				}

			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Println("ERROR", err)
			}
		}
	}()

	// Walk the directory tree and add each directory to the watcher.
	log.Println("Walking directory:", watchDir)
	if err := filepath.Walk(watchDir, func(path string, fi os.FileInfo, err error) error {
		// Skip .git directories
		if fi.IsDir() && strings.Contains(path, ".git") {
			return filepath.SkipDir
		}
		if fi.IsDir() {
			if err := watcher.Add(path); err != nil {
				return err
			}
			log.Println("Watching:", path)
		}
		return nil
	}); err != nil {
		log.Fatal("ERROR", err)
	}

	// Block main goroutine forever.
	<-make(chan struct{})
}
