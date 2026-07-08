package chunk

import (
	"math"
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

	ticker       *time.Ticker
	stopCh       chan struct{}
	lastActivity time.Time

	baseInterval    time.Duration
	currentInterval time.Duration
	maxInterval     time.Duration
	backoffFactor   float64
}

type ManagerOptions struct {
	BaseInterval  time.Duration
	MaxInterval   time.Duration
	BackoffFactor float64
}

func DefaultManagerOptions() ManagerOptions {
	return ManagerOptions{
		BaseInterval:  2 * time.Minute,
		MaxInterval:   30 * time.Minute,
		BackoffFactor: 1.5,
	}
}

func NewManager(strategy ChunkStrategy, store ChunkStore, emitter EventEmitter, options ...ManagerOptions) *Manager {
	opts := DefaultManagerOptions()
	if len(options) > 0 {
		opts = normalizeManagerOptions(options[0])
	}

	return &Manager{
		strategy:        strategy,
		store:           store,
		emitter:         emitter,
		ticker:          time.NewTicker(opts.BaseInterval),
		stopCh:          make(chan struct{}),
		lastActivity:    time.Now(),
		baseInterval:    opts.BaseInterval,
		currentInterval: opts.BaseInterval,
		maxInterval:     opts.MaxInterval,
		backoffFactor:   opts.BackoffFactor,
	}
}

func normalizeManagerOptions(opts ManagerOptions) ManagerOptions {
	defaults := DefaultManagerOptions()

	if opts.BaseInterval <= 0 {
		opts.BaseInterval = defaults.BaseInterval
	}
	if opts.MaxInterval <= 0 {
		opts.MaxInterval = defaults.MaxInterval
	}
	if opts.MaxInterval < opts.BaseInterval {
		opts.MaxInterval = opts.BaseInterval
	}
	if opts.BackoffFactor <= 1.0 {
		opts.BackoffFactor = defaults.BackoffFactor
	}

	return opts
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

	m.lastActivity = time.Now()

	if m.currentInterval != m.baseInterval {
		m.currentInterval = m.baseInterval
		m.ticker.Reset(m.baseInterval)
	}

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
			m.mu.Lock()
			m.flushStaleChunksLocked()

			timeSinceActivity := time.Since(m.lastActivity)
			if timeSinceActivity >= m.currentInterval {
				m.flushAllChunksLocked()
				m.backoff()
			}

			m.mu.Unlock()
		case <-m.stopCh:
			return
		}
	}
}

func (m *Manager) backoff() {
	next := time.Duration(math.Round(float64(m.currentInterval) * m.backoffFactor))
	if next > m.maxInterval {
		next = m.maxInterval
	}
	if next != m.currentInterval {
		m.currentInterval = next
		m.ticker.Reset(next)
	}
}

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

func (m *Manager) flushAllChunksLocked() {
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

func (m *Manager) FlushAll() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.flushAllChunksLocked()
	return nil
}

func (m *Manager) FlushStatus() (interval time.Duration, isIdle bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.currentInterval, m.currentInterval > m.baseInterval
}
