package core

import (
	"gurt/internal/chunk"
	"gurt/internal/store"
	"log"
	"time"
)

type Engine struct {
	chunkManager *chunk.Manager
	store        chunk.ChunkStore
}

type SimpleEventEmitter struct{}

func (e *SimpleEventEmitter) EmitChunkCreated(c chunk.Chunk) {
	log.Printf("Chunk created: %s for file %s", c.ID, c.FilePath)
}

func (e *SimpleEventEmitter) EmitChunkFlushed(chunks []chunk.Chunk) {
	log.Printf("Flushed %d chunks", len(chunks))
}

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

func (e *Engine) Start() {
	e.chunkManager.Start()
}

func (e *Engine) Stop() {
	e.chunkManager.Stop()
}

func (e *Engine) OnFileChange(path string, contents []byte) {
	event := chunk.FileChangeEvent{
		Path:     path,
		Contents: contents,
		Time:     time.Now(),
	}
	e.chunkManager.OnFileChange(event)
}

func (e *Engine) ForceFlush(filePath string) error {
	return e.chunkManager.ForceFlush(filePath)
}
