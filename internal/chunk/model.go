package chunk

import "time"

// Chunk represents a discrete unit of file changes tracked by Carya (diff, timing information, and metadata about changes)
type Chunk struct {
	ID           ChunkID   // Unique identifier for this chunk
	FilePath     string    // Path to the file this chunk represents
	Diff         string    // The actual diff content
	StartTime    time.Time // When the chunk period started
	EndTime      time.Time // When the chunk period ended
	FeatureLabel string    // Optional feature grouping label
	Hash         ChunkHash // Hash of the chunk content for integrity
	Manual       bool      // Whether this chunk was manually created
}

// RecordedBefore reports whether a was recorded before b. A file's next chunk
// only starts after its previous one is flushed, and is diffed on top of it,
// so this is the order a file's chunks must be applied in. StartTime (the
// first change a chunk saw) decides it, with EndTime and ID breaking ties.
// The store's own created_at column has one-second resolution, so store order
// cannot be relied on.
func RecordedBefore(a, b Chunk) bool {
	if !a.StartTime.Equal(b.StartTime) {
		return a.StartTime.Before(b.StartTime)
	}
	if !a.EndTime.Equal(b.EndTime) {
		return a.EndTime.Before(b.EndTime)
	}
	return a.ID < b.ID
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
