package chunk

import "time"

type Chunk struct {
	ID        ChunkID
	FilePath  string
	Diff      string
	StartTime time.Time
	EndTime   time.Time
	// FeatureTag feature.Tag // where we will implement feature tagging
	Hash   ChunkHash
	Manual bool
}

type FileChange struct {
	Timestame time.time
	Contents  []byte
}

type ChunkID string

type ChunkHash string
