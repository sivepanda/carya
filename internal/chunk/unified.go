package chunk

import (
	"crypto/sha256"
	"fmt"
	"sync"
	"time"
)

const (
	// DefaultFlushTimeout is the default time after which inactive chunks are flushed.
	DefaultFlushTimeout = 15 * time.Minute
)

// UnifiedStrategy implements a chunking strategy that groups file changes by time periods.
type UnifiedStrategy struct {
	mu           sync.RWMutex            // Protects concurrent access
	activeChunks map[string]*activeChunk // Active chunks by file path
	flushTimeout time.Duration           // Time before chunks are considered stale
}

// activeChunk tracks an in-progress chunk for a file.
type activeChunk struct {
	chunk       *Chunk    // The chunk being built
	lastUpdate  time.Time // When this chunk was last updated
	initialHash string    // Hash of the initial file content
}

// NewUnifiedStrategy creates a new unified chunking strategy with default settings.
func NewUnifiedStrategy() *UnifiedStrategy {
	return &UnifiedStrategy{
		activeChunks: make(map[string]*activeChunk),
		flushTimeout: DefaultFlushTimeout,
	}
}

// OnFileChange processes a file change event, creating or updating chunks as needed.
func (s *UnifiedStrategy) OnFileChange(event FileChangeEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()

	contentHash := s.hashContent(event.Contents)

	active, exists := s.activeChunks[event.Path]
	if !exists {
		s.activeChunks[event.Path] = &activeChunk{
			chunk: &Chunk{
				ID:        ChunkID(fmt.Sprintf("%s-%d", event.Path, event.Time.Unix())),
				FilePath:  event.Path,
				StartTime: event.Time,
				EndTime:   event.Time,
				Hash:      ChunkHash(contentHash),
				Manual:    false,
			},
			lastUpdate:  event.Time,
			initialHash: contentHash,
		}
		return
	}

	if active.initialHash == contentHash {
		return
	}

	active.chunk.EndTime = event.Time
	active.lastUpdate = event.Time
	active.chunk.Hash = ChunkHash(contentHash)
}

// FlushStaleChunks returns chunks that haven't been updated within the flush timeout.
func (s *UnifiedStrategy) FlushStaleChunks(now time.Time) []Chunk {
	s.mu.Lock()
	defer s.mu.Unlock()

	var flushed []Chunk
	for path, active := range s.activeChunks {
		if now.Sub(active.lastUpdate) >= s.flushTimeout {
			active.chunk.Diff = s.generateDiff(active.chunk)
			flushed = append(flushed, *active.chunk)
			delete(s.activeChunks, path)
		}
	}

	return flushed
}

// ForceFlush immediately creates a chunk for the specified file path.
func (s *UnifiedStrategy) ForceFlush(filePath string) *Chunk {
	s.mu.Lock()
	defer s.mu.Unlock()

	active, exists := s.activeChunks[filePath]
	if !exists {
		return nil
	}

	active.chunk.Manual = true
	active.chunk.Diff = s.generateDiff(active.chunk)
	chunk := *active.chunk
	delete(s.activeChunks, filePath)

	return &chunk
}

// hashContent generates a SHA256 hash of the given content.
func (s *UnifiedStrategy) hashContent(content []byte) string {
	hash := sha256.Sum256(content)
	return fmt.Sprintf("%x", hash)
}

// generateDiff creates a simple diff representation for a chunk.
func (s *UnifiedStrategy) generateDiff(chunk *Chunk) string {
	return fmt.Sprintf("File: %s\nTime: %s - %s\nHash: %s",
		chunk.FilePath,
		chunk.StartTime.Format(time.RFC3339),
		chunk.EndTime.Format(time.RFC3339),
		chunk.Hash)
}
