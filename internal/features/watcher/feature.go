package watcher

import (
	"carya/internal/engine"
	"carya/internal/repository"
	"carya/internal/watcher"
)

// WatcherFeature manages file watching functionality
type WatcherFeature struct {
	watcher *watcher.Watcher
	repo    *repository.Repository
}

// NewWatcherFeature creates a new watcher feature instance
func NewWatcherFeature() *WatcherFeature {
	return &WatcherFeature{}
}

// Name returns the feature name
func (wf *WatcherFeature) Name() string {
	return "watcher"
}

// Description returns a human-readable description
func (wf *WatcherFeature) Description() string {
	return "File system watcher for automatic change detection"
}

// Initialize sets up the file watcher with the given engine
func (wf *WatcherFeature) Initialize(repo *repository.Repository) error {
	wf.repo = repo
	return nil
}

// InitializeWithEngine sets up the file watcher with a specific engine
func (wf *WatcherFeature) InitializeWithEngine(repo *repository.Repository, eng *engine.Engine) error {
	wf.repo = repo

	fileWatcher, err := watcher.New(eng)
	if err != nil {
		return err
	}
	wf.watcher = fileWatcher
	return nil
}

// Start begins file watching
func (wf *WatcherFeature) Start() error {
	if wf.watcher != nil {
		return wf.watcher.Start(wf.repo.RootPath())
	}
	return nil
}

// Stop gracefully shuts down the file watcher
func (wf *WatcherFeature) Stop() error {
	if wf.watcher != nil {
		wf.watcher.Stop()
	}
	return nil
}

// Watcher returns the underlying watcher instance
func (wf *WatcherFeature) Watcher() *watcher.Watcher {
	return wf.watcher
}
