package features

import "carya/internal/repository"

// Feature defines the interface that all features must implement
type Feature interface {
	// Name returns the feature name
	Name() string

	// Description returns a human-readable description
	Description() string

	// Initialize sets up the feature for the given repository
	Initialize(repo *repository.Repository) error

	// Start begins the feature's operation (if applicable)
	Start() error

	// Stop gracefully shuts down the feature (if applicable)
	Stop() error
}

// FeatureManager manages the lifecycle of features
type FeatureManager struct {
	features []Feature
	repo     *repository.Repository
}

// NewFeatureManager creates a new feature manager
func NewFeatureManager(repo *repository.Repository) *FeatureManager {
	return &FeatureManager{
		features: make([]Feature, 0),
		repo:     repo,
	}
}

// Register adds a feature to the manager
func (fm *FeatureManager) Register(feature Feature) {
	fm.features = append(fm.features, feature)
}

// InitializeAll initializes all registered features
func (fm *FeatureManager) InitializeAll() error {
	for _, feature := range fm.features {
		if err := feature.Initialize(fm.repo); err != nil {
			return err
		}
	}
	return nil
}

// StartAll starts all registered features
func (fm *FeatureManager) StartAll() error {
	for _, feature := range fm.features {
		if err := feature.Start(); err != nil {
			return err
		}
	}
	return nil
}

// StopAll stops all registered features
func (fm *FeatureManager) StopAll() error {
	for _, feature := range fm.features {
		if err := feature.Stop(); err != nil {
			return err
		}
	}
	return nil
}
