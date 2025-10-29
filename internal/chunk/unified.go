package chunk

import (
	"crypto/sha256"
	"fmt"
	"log"
	"strings"
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
	chunk          *Chunk    // The chunk being built
	lastUpdate     time.Time // When this chunk was last updated
	initialHash    string    // Hash of the initial file content
	initialContent []byte    // Initial file content for diff generation
	latestContent  []byte    // Latest file content for diff generation
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
		// Create a copy of the content to avoid retention issues
		contentCopy := make([]byte, len(event.Contents))
		copy(contentCopy, event.Contents)

		s.activeChunks[event.Path] = &activeChunk{
			chunk: &Chunk{
				ID:        ChunkID(fmt.Sprintf("%s-%d", event.Path, event.Time.Unix())),
				FilePath:  event.Path,
				StartTime: event.Time,
				EndTime:   event.Time,
				Hash:      ChunkHash(contentHash),
				Manual:    false,
			},
			lastUpdate:     event.Time,
			initialHash:    contentHash,
			initialContent: contentCopy,
			latestContent:  contentCopy,
		}
		log.Printf("Started tracking changes: %s", event.Path)
		return
	}

	if active.initialHash == contentHash {
		log.Printf("Ignoring unchanged file: %s", event.Path)
		return
	}

	// Create a copy of the latest content
	contentCopy := make([]byte, len(event.Contents))
	copy(contentCopy, event.Contents)

	active.chunk.EndTime = event.Time
	active.lastUpdate = event.Time
	active.chunk.Hash = ChunkHash(contentHash)
	active.latestContent = contentCopy
	log.Printf("Updated chunk: %s (hash changed)", event.Path)
}

// FlushStaleChunks returns chunks that haven't been updated within the flush timeout.
func (s *UnifiedStrategy) FlushStaleChunks(now time.Time) []Chunk {
	s.mu.Lock()
	defer s.mu.Unlock()

	var flushed []Chunk
	for path, active := range s.activeChunks {
		if now.Sub(active.lastUpdate) >= s.flushTimeout {
			active.chunk.Diff = s.generateDiff(active)
			flushed = append(flushed, *active.chunk)
			delete(s.activeChunks, path)
		}
	}

	return flushed
}

// FlushAll immediately flushes all active chunks regardless of age.
func (s *UnifiedStrategy) FlushAll() []Chunk {
	s.mu.Lock()
	defer s.mu.Unlock()

	var flushed []Chunk
	for path, active := range s.activeChunks {
		active.chunk.Diff = s.generateDiff(active)
		flushed = append(flushed, *active.chunk)
		delete(s.activeChunks, path)
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
	active.chunk.Diff = s.generateDiff(active)
	chunk := *active.chunk
	delete(s.activeChunks, filePath)

	return &chunk
}

// hashContent generates a SHA256 hash of the given content.
func (s *UnifiedStrategy) hashContent(content []byte) string {
	hash := sha256.Sum256(content)
	return fmt.Sprintf("%x", hash)
}

// generateDiff creates a unified diff representation for a chunk.
func (s *UnifiedStrategy) generateDiff(active *activeChunk) string {
	chunk := active.chunk

	// Create header
	header := fmt.Sprintf("diff --git a/%s b/%s\nindex %s..%s\n--- a/%s\n+++ b/%s\n",
		chunk.FilePath,
		chunk.FilePath,
		active.initialHash[:8],
		string(chunk.Hash)[:8],
		chunk.FilePath,
		chunk.FilePath)

	// Generate line-by-line diff
	oldLines := splitLines(string(active.initialContent))
	newLines := splitLines(string(active.latestContent))

	diff := computeSimpleDiff(oldLines, newLines)

	return header + diff
}

// splitLines splits text into lines, preserving empty lines
func splitLines(text string) []string {
	if text == "" {
		return []string{}
	}
	lines := []string{}
	start := 0
	for i := 0; i < len(text); i++ {
		if text[i] == '\n' {
			lines = append(lines, text[start:i])
			start = i + 1
		}
	}
	if start < len(text) {
		lines = append(lines, text[start:])
	}
	return lines
}

// computeSimpleDiff creates a simple unified diff between two sets of lines
func computeSimpleDiff(oldLines, newLines []string) string {
	var result []string

	// Simple implementation: show all removed lines, then all added lines
	// This isn't a true LCS diff but works for basic visualization
	maxLen := len(oldLines)
	if len(newLines) > maxLen {
		maxLen = len(newLines)
	}

	if maxLen == 0 {
		return ""
	}

	// Create a simple hunk header
	result = append(result, fmt.Sprintf("@@ -%d,%d +%d,%d @@", 1, len(oldLines), 1, len(newLines)))

	// Find common prefix
	commonPrefix := 0
	for commonPrefix < len(oldLines) && commonPrefix < len(newLines) && oldLines[commonPrefix] == newLines[commonPrefix] {
		commonPrefix++
	}

	// Find common suffix
	commonSuffix := 0
	for commonSuffix < (len(oldLines)-commonPrefix) && commonSuffix < (len(newLines)-commonPrefix) &&
		oldLines[len(oldLines)-1-commonSuffix] == newLines[len(newLines)-1-commonSuffix] {
		commonSuffix++
	}

	// Show some context before changes
	contextStart := commonPrefix - 3
	if contextStart < 0 {
		contextStart = 0
	}
	for i := contextStart; i < commonPrefix; i++ {
		result = append(result, " "+oldLines[i])
	}

	// Show removed lines
	for i := commonPrefix; i < len(oldLines)-commonSuffix; i++ {
		result = append(result, "-"+oldLines[i])
	}

	// Show added lines
	for i := commonPrefix; i < len(newLines)-commonSuffix; i++ {
		result = append(result, "+"+newLines[i])
	}

	// Show some context after changes
	contextEnd := len(oldLines) - commonSuffix + 3
	if contextEnd > len(oldLines) {
		contextEnd = len(oldLines)
	}
	for i := len(oldLines) - commonSuffix; i < contextEnd; i++ {
		result = append(result, " "+oldLines[i])
	}

	return strings.Join(result, "\n")
}
