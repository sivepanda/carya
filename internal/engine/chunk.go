// Package engine provides the main engine and coordination logic for the Carya
// version control system, integrating chunk management, storage, and file watching.
package engine

import (
	"carya/internal/chunk"
	"carya/internal/store"
	"log"
	"time"
)

// Engine is the main coordination component of Carya that manages chunk creation,
// storage, and file change processing.
type Engine struct {
	chunkManager *chunk.Manager   // Manages chunk lifecycle and creation
	store        chunk.ChunkStore // Storage backend for chunks
}

// SimpleEventEmitter provides basic logging-based event emission for chunk events.
type SimpleEventEmitter struct{}

// EmitChunkCreated logs when a new chunk is created.
func (e *SimpleEventEmitter) EmitChunkCreated(c chunk.Chunk) {
	log.Printf("Chunk created: %s for file %s", c.ID, c.FilePath)
}

// EmitChunkFlushed logs when chunks are flushed to storage.
func (e *SimpleEventEmitter) EmitChunkFlushed(chunks []chunk.Chunk) {
	log.Printf("Flushed %d chunks", len(chunks))
}

// NewEngine creates a new Carya engine with SQLite storage at the specified path.
// It initializes the chunk manager with a unified strategy and simple event emitter.
func NewEngine(storePath string) (*Engine, error) {
	chunkStore, err := store.NewSQLiteStore(storePath)
	if err != nil {
		return nil, err
	}

	strategy := chunk.NewUnifiedStrategy()
	emitter := &SimpleEventEmitter{}
	manager := chunk.NewManager(strategy, chunkStore, emitter)

	return &Engine{
		chunkManager: manager,
		store:        chunkStore,
	}, nil
}

// Start begins the engine's background processing, including chunk management.
func (e *Engine) Start() {
	e.chunkManager.Start()
}

// Stop gracefully shuts down the engine and all its components.
func (e *Engine) Stop() {
	e.chunkManager.Stop()
}

// OnFileChange processes a file change event by creating a FileChangeEvent
// and passing it to the chunk manager for processing.
func (e *Engine) OnFileChange(path string, contents []byte) {
	event := chunk.FileChangeEvent{
		Path:     path,
		Contents: contents,
		Time:     time.Now(),
	}
	e.chunkManager.OnFileChange(event)
}

// ForceFlush immediately creates and saves a chunk for the specified file path.
// Returns an error if the chunk cannot be created or saved.
func (e *Engine) ForceFlush(filePath string) error {
	return e.chunkManager.ForceFlush(filePath)
}

// FlushAll immediately flushes all active chunks to storage.
func (e *Engine) FlushAll() error {
	return e.chunkManager.FlushAll()
}
