package engine

import (
	"carya/internal/engine"
	"carya/internal/repository"
)

// EngineFeature manages the main engine functionality
type EngineFeature struct {
	engine *engine.Engine
}

// NewEngineFeature creates a new engine feature instance
func NewEngineFeature() *EngineFeature {
	return &EngineFeature{}
}

// Name returns the feature name
func (ef *EngineFeature) Name() string {
	return "engine"
}

// Description returns a human-readable description
func (ef *EngineFeature) Description() string {
	return "Main engine for chunk management and storage"
}

// Initialize sets up the engine
func (ef *EngineFeature) Initialize(repo *repository.Repository) error {
	eng, err := engine.NewEngine(repo.DBPath())
	if err != nil {
		return err
	}
	ef.engine = eng
	return nil
}

// Start begins the engine operation
func (ef *EngineFeature) Start() error {
	if ef.engine != nil {
		ef.engine.Start()
	}
	return nil
}

// Stop gracefully shuts down the engine
func (ef *EngineFeature) Stop() error {
	if ef.engine != nil {
		ef.engine.Stop()
	}
	return nil
}

// Engine returns the underlying engine instance
func (ef *EngineFeature) Engine() *engine.Engine {
	return ef.engine
}
