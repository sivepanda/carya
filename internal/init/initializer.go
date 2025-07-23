package init

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"carya/internal/features/engine"
	"carya/internal/features/watcher"
	"carya/internal/repository"
)

// Initializer manages the initialization process for a new Carya repository
type Initializer struct {
	repo           *repository.Repository
	engineFeature  *engine.EngineFeature
	watcherFeature *watcher.WatcherFeature
}

// NewInitializer creates a new initializer with default features
func NewInitializer() (*Initializer, error) {
	repo, err := repository.New()
	if err != nil {
		return nil, fmt.Errorf("failed to create repository: %w", err)
	}

	return &Initializer{
		repo:           repo,
		engineFeature:  engine.NewEngineFeature(),
		watcherFeature: watcher.NewWatcherFeature(),
	}, nil
}

// Initialize sets up the repository and all features
func (i *Initializer) Initialize() error {
	fmt.Println("Initializing Carya repository...")

	// Create .carya directory
	if err := i.repo.EnsureExists(); err != nil {
		return fmt.Errorf("failed to create repository: %w", err)
	}

	// Initialize engine feature
	if err := i.engineFeature.Initialize(i.repo); err != nil {
		return fmt.Errorf("failed to initialize engine: %w", err)
	}

	// Initialize watcher feature with engine
	if err := i.watcherFeature.InitializeWithEngine(i.repo, i.engineFeature.Engine()); err != nil {
		return fmt.Errorf("failed to initialize watcher: %w", err)
	}

	return nil
}

// Run starts the initialized system
func (i *Initializer) Run() error {
	// Start engine
	if err := i.engineFeature.Start(); err != nil {
		return fmt.Errorf("failed to start engine: %w", err)
	}
	defer i.engineFeature.Stop()

	// Start watcher
	if err := i.watcherFeature.Start(); err != nil {
		return fmt.Errorf("failed to start watcher: %w", err)
	}
	defer i.watcherFeature.Stop()

	fmt.Println("Carya is now watching for file changes...")
	fmt.Println("Press Ctrl+C to stop")

	// Wait for interrupt signal
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c

	fmt.Println("\nShutting down Carya...")
	return nil
}
