package init

import (
	"bufio"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"carya/internal/features/engine"
	"carya/internal/features/watcher"
	"carya/internal/repository"
)

// Initializer manages the initialization process for a new Carya repository
type Initializer struct {
	repo             *repository.Repository
	enabledFeatures  []string
	engineFeature    *engine.EngineFeature
	watcherFeature   *watcher.WatcherFeature
}

// NewInitializer creates a new initializer with specified features
// enabledFeatures should contain feature keys like "featcom", "housekeep"
func NewInitializer(enabledFeatures []string) (*Initializer, error) {
	repo, err := repository.New()
	if err != nil {
		return nil, fmt.Errorf("failed to create repository: %w", err)
	}

	return &Initializer{
		repo:            repo,
		enabledFeatures: enabledFeatures,
	}, nil
}

// isFeatureEnabled checks if a feature key is in the enabled features list
func (i *Initializer) isFeatureEnabled(featureKey string) bool {
	for _, key := range i.enabledFeatures {
		if key == featureKey {
			return true
		}
	}
	return false
}

// ensureGitignore ensures .carya/ is in .gitignore
func (i *Initializer) ensureGitignore() error {
	gitignorePath := ".gitignore"
	caryaEntry := ".carya/"

	// Check if .gitignore exists
	content := ""
	if data, err := os.ReadFile(gitignorePath); err == nil {
		content = string(data)

		// Check if .carya/ is already in .gitignore
		scanner := bufio.NewScanner(strings.NewReader(content))
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == caryaEntry || line == ".carya" {
				// Already present
				return nil
			}
		}
	}

	// Add .carya/ to .gitignore
	f, err := os.OpenFile(gitignorePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open .gitignore: %w", err)
	}
	defer f.Close()

	// Add newline before entry if file doesn't end with one
	if len(content) > 0 && !strings.HasSuffix(content, "\n") {
		if _, err := f.WriteString("\n"); err != nil {
			return fmt.Errorf("failed to write to .gitignore: %w", err)
		}
	}

	// Add comment and entry
	if len(content) == 0 {
		// New file, add header
		if _, err := f.WriteString("# Carya directory\n"); err != nil {
			return fmt.Errorf("failed to write to .gitignore: %w", err)
		}
	}

	if _, err := f.WriteString(caryaEntry + "\n"); err != nil {
		return fmt.Errorf("failed to write to .gitignore: %w", err)
	}

	return nil
}

// Initialize sets up the repository and all features
func (i *Initializer) Initialize() error {
	fmt.Println("Initializing Carya repository...")

	// Create .carya directory
	if err := i.repo.EnsureExists(); err != nil {
		return fmt.Errorf("failed to create repository: %w", err)
	}

	fmt.Println("Created .carya directory")

	// Ensure .carya/ is in .gitignore
	if err := i.ensureGitignore(); err != nil {
		// Don't fail the init, just warn
		fmt.Printf("Warning: Could not update .gitignore: %v\n", err)
	} else {
		fmt.Println("Added .carya/ to .gitignore")
	}

	// Initialize features based on user selection
	if i.isFeatureEnabled("featcom") {
		fmt.Println("Initializing feature-based commits...")

		// Initialize engine feature
		i.engineFeature = engine.NewEngineFeature()
		if err := i.engineFeature.Initialize(i.repo); err != nil {
			return fmt.Errorf("failed to initialize engine: %w", err)
		}

		// Initialize watcher feature with engine
		i.watcherFeature = watcher.NewWatcherFeature()
		if err := i.watcherFeature.InitializeWithEngine(i.repo, i.engineFeature.Engine()); err != nil {
			return fmt.Errorf("failed to initialize watcher: %w", err)
		}

		fmt.Println("✓ Feature-based commits enabled")
	}

	if i.isFeatureEnabled("housekeep") {
		fmt.Println("Initializing automated housekeeping...")
		// TODO: Initialize housekeeping feature when implemented
		fmt.Println("✓ Housekeeping configuration ready")
	}

	if len(i.enabledFeatures) == 0 {
		fmt.Println("Basic Carya repository initialized (no features enabled)")
	}

	return nil
}

// Run starts the initialized system (only if features are enabled)
func (i *Initializer) Run() error {
	// Only run if we have features to run
	if !i.isFeatureEnabled("featcom") {
		fmt.Println("No background features to run. Repository is ready!")
		return nil
	}

	// Start engine
	if i.engineFeature != nil {
		if err := i.engineFeature.Start(); err != nil {
			return fmt.Errorf("failed to start engine: %w", err)
		}
		defer i.engineFeature.Stop()
	}

	// Start watcher
	if i.watcherFeature != nil {
		if err := i.watcherFeature.Start(); err != nil {
			return fmt.Errorf("failed to start watcher: %w", err)
		}
		defer i.watcherFeature.Stop()
	}

	fmt.Println("Carya is now watching for file changes...")
	fmt.Println("Press Ctrl+C to stop")

	// Wait for interrupt signal
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c

	fmt.Println("\nShutting down Carya...")
	return nil
}
