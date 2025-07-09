package core

import "time"

type Chunk struct {
	ID        int       `json:"id"`
	FilePath  string    `json:"file_path"`
	Changes   string    `json:"changes"`
	CreatedAt time.Time `json:"created_at"`
}
