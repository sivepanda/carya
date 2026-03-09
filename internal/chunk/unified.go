package chunk

import (
	"bytes"
	"crypto/sha1"
	"fmt"
	"log"
	"sync"
	"time"

	"carya/internal/git"
)

const (
	// DefaultFlushTimeout is the default time after which inactive chunks are flushed.
	DefaultFlushTimeout = 15 * time.Minute
)

// UnifiedStrategy implements a chunking strategy that groups file changes by time periods.
// It uses git blobs for content storage instead of in-memory storage.
type UnifiedStrategy struct {
	mu           sync.RWMutex            // Protects concurrent access
	activeChunks map[string]*activeChunk // Active chunks by file path
	flushTimeout time.Duration           // Time before chunks are considered stale
	shadow       *git.ShadowRepo         // Shadow git repository for blob storage
}

// activeChunk tracks an in-progress chunk for a file using git blob hashes.
type activeChunk struct {
	chunk           *Chunk    // The chunk being built
	lastUpdate      time.Time // When this chunk was last updated
	initialBlobHash string    // Git blob hash of initial content
	latestBlobHash  string    // Git blob hash of latest content
}

// NewUnifiedStrategy creates a new unified chunking strategy with default settings.
// If shadow is nil, falls back to basic hash tracking without diffs.
func NewUnifiedStrategy(shadow *git.ShadowRepo) *UnifiedStrategy {
	return &UnifiedStrategy{
		activeChunks: make(map[string]*activeChunk),
		flushTimeout: DefaultFlushTimeout,
		shadow:       shadow,
	}
}

// OnFileChange processes a file change event, creating or updating chunks as needed.
func (s *UnifiedStrategy) OnFileChange(event FileChangeEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Store content as git blob
	blobHash, err := s.hashContent(event.Contents)
	if err != nil {
		log.Printf("Failed to hash content for %s: %v", event.Path, err)
		return
	}

	active, exists := s.activeChunks[event.Path]
	if !exists {
		// Use the shadow index entry (seeded from HEAD) as the baseline.
		// For new files not in HEAD, baselineHash falls back to the new content hash.
		baselineHash := blobHash
		if s.shadow != nil {
			if existing, _ := s.shadow.GetIndexEntry(event.Path); existing != "" {
				baselineHash = existing
			}
		}

		s.activeChunks[event.Path] = &activeChunk{
			chunk: &Chunk{
				ID:        ChunkID(fmt.Sprintf("%s-%d", event.Path, event.Time.Unix())),
				FilePath:  event.Path,
				StartTime: event.Time,
				EndTime:   event.Time,
				Hash:      ChunkHash(blobHash),
				Manual:    false,
			},
			lastUpdate:      event.Time,
			initialBlobHash: baselineHash,
			latestBlobHash:  blobHash,
		}

		if s.shadow != nil {
			if err := s.shadow.UpdateIndex(event.Path, blobHash, "100644"); err != nil {
				log.Printf("Failed to update shadow index for %s: %v", event.Path, err)
			}
		}

		log.Printf("Started tracking changes: %s", event.Path)
		return
	}

	if active.initialBlobHash == blobHash {
		log.Printf("Ignoring unchanged file: %s", event.Path)
		return
	}

	active.chunk.EndTime = event.Time
	active.lastUpdate = event.Time
	active.chunk.Hash = ChunkHash(blobHash)
	active.latestBlobHash = blobHash

	// Update shadow repo index
	if s.shadow != nil {
		if err := s.shadow.UpdateIndex(event.Path, blobHash, "100644"); err != nil {
			log.Printf("Failed to update shadow index for %s: %v", event.Path, err)
		}
	}

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

// hashContent stores content as a git blob and returns its hash.
// Falls back to SHA256 if shadow repo is not available.
func (s *UnifiedStrategy) hashContent(content []byte) (string, error) {
	if s.shadow != nil {
		return s.shadow.HashObject(content)
	}

	// Fallback: compute hash using git's blob hashing algorithm (SHA1)
	blobHeader := fmt.Sprintf("blob %d\x00%s", len(content), content)
	h := sha1.Sum([]byte(blobHeader))
	return fmt.Sprintf("%x", h), nil
}

// generateDiff creates a unified diff representation for a chunk using git diff.
func (s *UnifiedStrategy) generateDiff(active *activeChunk) string {
	chunk := active.chunk

	// If no shadow repo, generate a simple diff header
	if s.shadow == nil {
		return fmt.Sprintf("diff --git a/%s b/%s\nindex %s..%s\n--- a/%s\n+++ b/%s\n@@ changes not available (no shadow repo) @@\n",
			chunk.FilePath,
			chunk.FilePath,
			active.initialBlobHash[:8],
			active.latestBlobHash[:8],
			chunk.FilePath,
			chunk.FilePath)
	}

	// Get content for binary check
	initialContent, _ := s.shadow.GetObjectContent(active.initialBlobHash)
	latestContent, _ := s.shadow.GetObjectContent(active.latestBlobHash)

	// Check if content is binary
	if isBinary(initialContent) || isBinary(latestContent) {
		return fmt.Sprintf("Binary file %s has changed\n(Initial hash: %s, Latest hash: %s)",
			chunk.FilePath,
			active.initialBlobHash[:8],
			active.latestBlobHash[:8])
	}

	// Use git diff for text files
	diff, err := s.shadow.DiffBlobsRaw(active.initialBlobHash, active.latestBlobHash)
	if err != nil {
		log.Printf("Failed to generate git diff for %s: %v", chunk.FilePath, err)
		// Fall back to header-only diff
		return fmt.Sprintf("diff --git a/%s b/%s\nindex %s..%s\n--- a/%s\n+++ b/%s\n@@ diff generation failed @@\n",
			chunk.FilePath,
			chunk.FilePath,
			active.initialBlobHash[:8],
			active.latestBlobHash[:8],
			chunk.FilePath,
			chunk.FilePath)
	}

	// If git diff returns empty but hashes differ, construct a basic diff header
	if diff == "" && active.initialBlobHash != active.latestBlobHash {
		return fmt.Sprintf("diff --git a/%s b/%s\nindex %s..%s\n--- a/%s\n+++ b/%s\n",
			chunk.FilePath,
			chunk.FilePath,
			active.initialBlobHash[:8],
			active.latestBlobHash[:8],
			chunk.FilePath,
			chunk.FilePath)
	}

	return diff
}

// isBinary checks if the content appears to be binary data.
func isBinary(content []byte) bool {
	if len(content) == 0 {
		return false
	}

	// Check for null bytes (strong indicator of binary data)
	if bytes.IndexByte(content, 0) != -1 {
		return true
	}

	// Check up to first 8KB for performance
	sampleSize := len(content)
	if sampleSize > 8192 {
		sampleSize = 8192
	}

	// Count non-printable characters
	nonPrintable := 0
	for i := 0; i < sampleSize; i++ {
		b := content[i]
		if b < 9 || (b > 13 && b < 32) || b > 126 {
			nonPrintable++
		}
	}

	return float64(nonPrintable)/float64(sampleSize) > 0.3
}

// WriteTree writes the current shadow index as a tree and returns its hash.
func (s *UnifiedStrategy) WriteTree() (string, error) {
	if s.shadow == nil {
		log.Printf("Shadow repo not initialized")
		return "", fmt.Errorf("shadow repo not initialized")
	}
	return s.shadow.WriteTree()
}

// GetShadow returns the shadow repository.
func (s *UnifiedStrategy) GetShadow() *git.ShadowRepo {
	return s.shadow
}
