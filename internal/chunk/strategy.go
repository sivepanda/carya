package chunk

import "time"

type ChunkStrategy interface {
	OnFileChange(event FileChangeEvent)

	FlushStaleChunks(now time.Time) []Chunk

	ForceFlush(filePath string) *Chunk
}

type FileChangeEvent struct {
	Path     string
	Contents []byte
	Time     time.Time
}
