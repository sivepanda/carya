package chunk

import (
	"crypto/sha256"
	"fmt"
	"sync"
	"time"
)

const (
	DefaultFlushTimeout = 15 * time.Minute
)

type UnifiedStrategy struct {
	mu           sync.RWMutex
	activeChunks map[string]*activeChunk
	flushTimeout time.Duration
}

type activeChunk struct {
	chunk       *Chunk
	lastUpdate  time.Time
	initialHash string
}

func NewUnifiedStrategy() *UnifiedStrategy {
	return &UnifiedStrategy{
		activeChunks: make(map[string]*activeChunk),
		flushTimeout: DefaultFlushTimeout,
	}
}

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

func (s *UnifiedStrategy) hashContent(content []byte) string {
	hash := sha256.Sum256(content)
	return fmt.Sprintf("%x", hash)
}

func (s *UnifiedStrategy) generateDiff(chunk *Chunk) string {
	return fmt.Sprintf("File: %s\nTime: %s - %s\nHash: %s",
		chunk.FilePath,
		chunk.StartTime.Format(time.RFC3339),
		chunk.EndTime.Format(time.RFC3339),
		chunk.Hash)
}
