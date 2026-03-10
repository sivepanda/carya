package chunk

import (
	"bytes"
	"crypto/sha1"
	"fmt"
	"log"
	"os"
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

	active, exists := s.activeChunks[event.Path]
	if event.Contents == nil {
		s.handleDelete(event, active, exists)
		return
	}

	// Store content as git blob
	blobHash, err := s.hashContent(event.Contents)
	if err != nil {
		log.Printf("Failed to hash content for %s: %v", event.Path, err)
		return
	}

	if !exists {
		// Use the shadow index entry (seeded from HEAD) as the baseline.
		// For files not in HEAD, keep an empty baseline so additions are tracked.
		baselineHash := ""
		if s.shadow != nil {
			baselineHash, _ = s.shadow.GetIndexEntry(event.Path)
		}

		if baselineHash != "" && baselineHash == blobHash {
			log.Printf("Ignoring unchanged file: %s", event.Path)
			return
		}

		s.activeChunks[event.Path] = &activeChunk{
			chunk: &Chunk{
				ID:        ChunkID(fmt.Sprintf("%s-%d", event.Path, event.Time.UnixNano())),
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
		if s.shadow != nil {
			if active.initialBlobHash == "" {
				_ = s.shadow.RemoveFromIndex(event.Path)
			} else {
				_ = s.shadow.UpdateIndex(event.Path, active.initialBlobHash, "100644")
			}
		}
		delete(s.activeChunks, event.Path)
		log.Printf("Discarded chunk with no net changes: %s", event.Path)
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
			shortHash(active.initialBlobHash),
			shortHash(active.latestBlobHash),
			chunk.FilePath,
			chunk.FilePath)
	}

	// Get content for binary check
	initialContent, _ := s.contentForHash(active.initialBlobHash)
	latestContent, _ := s.contentForHash(active.latestBlobHash)

	// Check if content is binary
	if isBinary(initialContent) || isBinary(latestContent) {
		return fmt.Sprintf("Binary file %s has changed\n(Initial hash: %s, Latest hash: %s)",
			chunk.FilePath,
			shortHash(active.initialBlobHash),
			shortHash(active.latestBlobHash))
	}

	// Use path-aware git diff for text files
	diff, err := s.diffWithPath(chunk.FilePath, initialContent, latestContent)
	if err != nil {
		log.Printf("Failed to generate git diff for %s: %v", chunk.FilePath, err)
		// Fall back to header-only diff
		return fmt.Sprintf("diff --git a/%s b/%s\nindex %s..%s\n--- a/%s\n+++ b/%s\n@@ diff generation failed @@\n",
			chunk.FilePath,
			chunk.FilePath,
			shortHash(active.initialBlobHash),
			shortHash(active.latestBlobHash),
			chunk.FilePath,
			chunk.FilePath)
	}

	// If git diff returns empty but hashes differ, construct a basic diff header
	if diff == "" && active.initialBlobHash != active.latestBlobHash {
		return fmt.Sprintf("diff --git a/%s b/%s\nindex %s..%s\n--- a/%s\n+++ b/%s\n",
			chunk.FilePath,
			chunk.FilePath,
			shortHash(active.initialBlobHash),
			shortHash(active.latestBlobHash),
			chunk.FilePath,
			chunk.FilePath)
	}

	return diff
}

func (s *UnifiedStrategy) handleDelete(event FileChangeEvent, active *activeChunk, exists bool) {
	baselineHash := ""
	if exists {
		baselineHash = active.initialBlobHash
	} else if s.shadow != nil {
		baselineHash, _ = s.shadow.GetIndexEntry(event.Path)
	}

	if baselineHash == "" {
		// Deleting an untracked file creates no meaningful chunk.
		return
	}

	if !exists {
		active = &activeChunk{
			chunk: &Chunk{
				ID:        ChunkID(fmt.Sprintf("%s-%d", event.Path, event.Time.UnixNano())),
				FilePath:  event.Path,
				StartTime: event.Time,
				EndTime:   event.Time,
				Hash:      ChunkHash(fmt.Sprintf("deleted-%d", event.Time.UnixNano())),
				Manual:    false,
			},
			lastUpdate:      event.Time,
			initialBlobHash: baselineHash,
			latestBlobHash:  "",
		}
		s.activeChunks[event.Path] = active
	} else {
		active.chunk.EndTime = event.Time
		active.lastUpdate = event.Time
		active.latestBlobHash = ""
		active.chunk.Hash = ChunkHash(fmt.Sprintf("deleted-%d", event.Time.UnixNano()))
	}

	if s.shadow != nil {
		_ = s.shadow.RemoveFromIndex(event.Path)
	}
}

func (s *UnifiedStrategy) contentForHash(hash string) ([]byte, error) {
	if hash == "" {
		return nil, nil
	}
	return s.shadow.GetObjectContent(hash)
}

func (s *UnifiedStrategy) diffWithPath(filePath string, oldContent, newContent []byte) (string, error) {
	oldPath := "/dev/null"
	newPath := "/dev/null"

	if oldContent != nil {
		f, err := os.CreateTemp("", "carya-old-*")
		if err != nil {
			return "", err
		}
		defer os.Remove(f.Name())
		if _, err := f.Write(oldContent); err != nil {
			_ = f.Close()
			return "", err
		}
		if err := f.Close(); err != nil {
			return "", err
		}
		oldPath = f.Name()
	}

	if newContent != nil {
		f, err := os.CreateTemp("", "carya-new-*")
		if err != nil {
			return "", err
		}
		defer os.Remove(f.Name())
		if _, err := f.Write(newContent); err != nil {
			_ = f.Close()
			return "", err
		}
		if err := f.Close(); err != nil {
			return "", err
		}
		newPath = f.Name()
	}

	diff, err := s.shadow.DiffFilesWithPath(oldPath, newPath, filePath)
	if err != nil {
		return "", err
	}

	return diff, nil
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

func shortHash(hash string) string {
	if len(hash) == 0 {
		return "00000000"
	}
	if len(hash) < 8 {
		return hash
	}
	return hash[:8]
}
