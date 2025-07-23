package chunk

import (
	"sync"
	"time"
)

// ChunkStore defines the interface for persisting and retrieving chunks.
type ChunkStore interface {
	// SaveChunk persists a chunk to the store.
	SaveChunk(chunk Chunk) error
	// FindChunks retrieves all chunks for a specific file path.
	FindChunks(filePath string) ([]Chunk, error)
	// GetRecentChunks retrieves the most recently created chunks up to the specified limit.
	GetRecentChunks(limit int) ([]Chunk, error)
}

// EventEmitter defines the interface for emitting chunk-related events.
type EventEmitter interface {
	// EmitChunkCreated notifies listeners that a new chunk has been created.
	EmitChunkCreated(chunk Chunk)
	// EmitChunkFlushed notifies listeners that chunks have been flushed to storage.
	EmitChunkFlushed(chunks []Chunk)
}

// Manager coordinates chunk creation, storage, and lifecycle management. It uses a ChunkStrategy to determine when to create chunks and manages periodic flushing of stale chunks.
type Manager struct {
	mu       sync.RWMutex  // Protects concurrent access to strategy
	strategy ChunkStrategy // Strategy for creating chunks
	store    ChunkStore    // Storage backend for chunks
	emitter  EventEmitter  // Event emitter for notifications
	ticker   *time.Ticker  // Timer for periodic flushing
	stopCh   chan struct{} // Channel to signal shutdown
}

// NewManager creates a new chunk manager with the specified strategy, store, and emitter. The manager will flush stale chunks every 5 minutes.
func NewManager(strategy ChunkStrategy, store ChunkStore, emitter EventEmitter) *Manager {
	return &Manager{
		strategy: strategy,
		store:    store,
		emitter:  emitter,
		ticker:   time.NewTicker(5 * time.Minute),
		stopCh:   make(chan struct{}),
	}
}

// Start begins the manager's background processing, including periodic flushing of stale chunks.
func (m *Manager) Start() {
	go m.flushLoop()
}

// Stop gracefully shuts down the manager, stopping all background processing.
func (m *Manager) Stop() {
	close(m.stopCh)
	if m.ticker != nil {
		m.ticker.Stop()
	}
}

// OnFileChange processes a file change event through the configured strategy.
func (m *Manager) OnFileChange(event FileChangeEvent) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.strategy.OnFileChange(event)
}

// ForceFlush immediately creates and saves a chunk for the specified file path.
// Returns an error if the chunk cannot be saved to the store.
func (m *Manager) ForceFlush(filePath string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	chunk := m.strategy.ForceFlush(filePath)
	if chunk == nil {
		return nil
	}

	if err := m.store.SaveChunk(*chunk); err != nil {
		return err
	}

	if m.emitter != nil {
		m.emitter.EmitChunkCreated(*chunk)
	}

	return nil
}

// flushLoop runs in a separate goroutine and periodically flushes stale chunks.
func (m *Manager) flushLoop() {
	for {
		select {
		case <-m.ticker.C:
			m.flushStaleChunks()
		case <-m.stopCh:
			return
		}
	}
}

// flushStaleChunks identifies and saves stale chunks to the store.
// Continues processing even if individual chunks fail to save.
func (m *Manager) flushStaleChunks() {
	m.mu.Lock()
	defer m.mu.Unlock()

	chunks := m.strategy.FlushStaleChunks(time.Now())
	if len(chunks) == 0 {
		return
	}

	for _, chunk := range chunks {
		if err := m.store.SaveChunk(chunk); err != nil {
			continue
		}
	}

	if m.emitter != nil {
		m.emitter.EmitChunkFlushed(chunks)
	}
}
