package chunk

import (
	"sync"
	"time"
)

//lorme upsum dolor

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
	mu           sync.RWMutex  // Protects concurrent access to strategy
	strategy     ChunkStrategy // Strategy for creating chunks
	store        ChunkStore    // Storage backend for chunks
	emitter      EventEmitter  // Event emitter for notifications
	ticker       *time.Ticker  // Timer for periodic flushing
	stopCh       chan struct{} // Channel to signal shutdown
	lastActivity time.Time     // Time of last file change
	isIdle       bool          // Whether system is in idle mode
	idleThreshold time.Duration // Time before considering system idle
	activeInterval time.Duration // Flush interval when active
	idleInterval time.Duration // Flush interval when idle
}

// NewManager creates a new chunk manager with the specified strategy, store, and emitter. The manager will flush stale chunks every 5 minutes when active, and every 30 minutes when idle.
func NewManager(strategy ChunkStrategy, store ChunkStore, emitter EventEmitter) *Manager {
	activeInterval := 5 * time.Minute
	return &Manager{
		strategy:       strategy,
		store:          store,
		emitter:        emitter,
		ticker:         time.NewTicker(activeInterval),
		stopCh:         make(chan struct{}),
		lastActivity:   time.Now(),
		isIdle:         false,
		idleThreshold:  5 * time.Minute,
		activeInterval: activeInterval,
		idleInterval:   30 * time.Minute,
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

	m.lastActivity = time.Now()

	// If we were idle, switch back to active mode
	if m.isIdle {
		m.switchToActiveMode()
	}

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
// Implements adaptive flushing: switches to idle mode after 5 minutes of inactivity.
func (m *Manager) flushLoop() {
	for {
		select {
		case <-m.ticker.C:
			m.mu.Lock()
			timeSinceActivity := time.Since(m.lastActivity)

			// Check if we should switch to idle mode
			if !m.isIdle && timeSinceActivity >= m.idleThreshold {
				// Aggressive idle flush: flush everything immediately
				m.flushAllChunksLocked()
				m.switchToIdleMode()
			} else if !m.isIdle {
				// Normal active mode: flush stale chunks only
				m.flushStaleChunksLocked()
			}
			// If already idle, just wait for activity (no flushing needed)

			m.mu.Unlock()
		case <-m.stopCh:
			return
		}
	}
}

// flushStaleChunksLocked identifies and saves stale chunks to the store.
// Continues processing even if individual chunks fail to save.
// Must be called with m.mu held.
func (m *Manager) flushStaleChunksLocked() {
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

// flushAllChunksLocked immediately flushes all active chunks to storage.
// Must be called with m.mu held.
func (m *Manager) flushAllChunksLocked() {
	// Check if strategy supports FlushAll
	type flushAller interface {
		FlushAll() []Chunk
	}

	fa, ok := m.strategy.(flushAller)
	if !ok {
		return
	}

	chunks := fa.FlushAll()
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

// FlushAll immediately flushes all active chunks to storage.
func (m *Manager) FlushAll() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.flushAllChunksLocked()
	return nil
}

// switchToIdleMode switches the ticker to idle mode (slower interval).
// Must be called with m.mu held.
func (m *Manager) switchToIdleMode() {
	if m.isIdle {
		return
	}
	m.isIdle = true
	m.ticker.Reset(m.idleInterval)
}

// switchToActiveMode switches the ticker to active mode (faster interval).
// Must be called with m.mu held.
func (m *Manager) switchToActiveMode() {
	if !m.isIdle {
		return
	}
	m.isIdle = false
	m.ticker.Reset(m.activeInterval)
}
