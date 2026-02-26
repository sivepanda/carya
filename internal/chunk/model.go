package chunk

import "time"

// Chunk represents a discrete unit of file changes tracked by Carya (diff, timing information, and metadata about changes)
type Chunk struct {
	ID        ChunkID   // Unique identifier for this chunk
	FilePath  string    // Path to the file this chunk represents
	Diff      string    // The actual diff content
	StartTime time.Time // When the chunk period started
	EndTime   time.Time // When the chunk period ended
	// FeatureTag feature.Tag // where we will implement feature tagging
	Hash   ChunkHash // Hash of the chunk content for integrity
	Manual bool      // Whether this chunk was manually created
}

// FileChange represents a single file modification event with its timestamp and content.
type FileChange struct {
	Timestamp time.Time // When the change occurred
	Contents  []byte    // The file contents at the time of change
}

// ChunkID is a unique identifier for a chunk.
type ChunkID string

// ChunkHash represents a hash of chunk content for integrity verification.
type ChunkHash string
