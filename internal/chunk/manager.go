package chunk

import (
	"sync"
	"time"
)

type ChunkStore interface {
	SaveChunk(chunk Chunk) error
	FindChunks(filePath string) ([]Chunk, error)
	GetRecentChunks(limit int) ([]Chunk, error)
}

type EventEmitter interface {
	EmitChunkCreated(chunk Chunk)
	EmitChunkFlushed(chunks []Chunk)
}

type Manager struct {
	mu       sync.RWMutex
	strategy ChunkStrategy
	store    ChunkStore
	emitter  EventEmitter
	ticker   *time.Ticker
	stopCh   chan struct{}
}

func NewManager(strategy ChunkStrategy, store ChunkStore, emitter EventEmitter) *Manager {
	return &Manager{
		strategy: strategy,
		store:    store,
		emitter:  emitter,
		ticker:   time.NewTicker(5 * time.Minute),
		stopCh:   make(chan struct{}),
	}
}

func (m *Manager) Start() {
	go m.flushLoop()
}

func (m *Manager) Stop() {
	close(m.stopCh)
	if m.ticker != nil {
		m.ticker.Stop()
	}
}

func (m *Manager) OnFileChange(event FileChangeEvent) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.strategy.OnFileChange(event)
}

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
