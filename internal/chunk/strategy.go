package chunk

import "time"

// ChunkStrategy defines the interface for different chunking strategies.
// Implementations determine how file changes are grouped into chunks.
type ChunkStrategy interface {
	// OnFileChange processes a file change event and updates internal state.
	OnFileChange(event FileChangeEvent)

	// FlushStaleChunks returns and removes chunks that have become stale based on the given time.
	// Stale chunks are those that haven't been updated for a certain period.
	FlushStaleChunks(now time.Time) []Chunk

	// ForceFlush immediately creates and returns a chunk for the specified file path.
	// Returns nil if no chunk can be created for the file.
	ForceFlush(filePath string) *Chunk
}

// FileChangeEvent represents a file modification event with its metadata.
type FileChangeEvent struct {
	Path     string    // Full path to the changed file
	Contents []byte    // Current contents of the file
	Time     time.Time // When the change occurred
}
