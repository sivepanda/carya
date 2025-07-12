package store

import (
	"database/sql"
	"gurt/internal/chunk"

	_ "github.com/mattn/go-sqlite3"
)

type SQLiteStore struct {
	db *sql.DB
}

func NewSQLiteStore(dataSourceName string) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite3", dataSourceName)
	if err != nil {
		return nil, err
	}

	store := &SQLiteStore{db: db}
	if err := store.initTables(); err != nil {
		return nil, err
	}

	return store, nil
}

func (s *SQLiteStore) initTables() error {
	query := `
		CREATE TABLE IF NOT EXISTS chunks (
			id TEXT PRIMARY KEY,
			file_path TEXT NOT NULL,
			diff TEXT NOT NULL,
			start_time TIMESTAMP NOT NULL,
			end_time TIMESTAMP NOT NULL,
			hash TEXT NOT NULL,
			manual BOOLEAN NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_chunks_file_path ON chunks(file_path);
		CREATE INDEX IF NOT EXISTS idx_chunks_created_at ON chunks(created_at);
	`
	_, err := s.db.Exec(query)
	return err
}

func (s *SQLiteStore) SaveChunk(c chunk.Chunk) error {
	query := `
		INSERT OR REPLACE INTO chunks (id, file_path, diff, start_time, end_time, hash, manual)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`
	_, err := s.db.Exec(query, c.ID, c.FilePath, c.Diff, c.StartTime, c.EndTime, c.Hash, c.Manual)
	return err
}

func (s *SQLiteStore) FindChunks(filePath string) ([]chunk.Chunk, error) {
	query := `
		SELECT id, file_path, diff, start_time, end_time, hash, manual
		FROM chunks 
		WHERE file_path = ?
		ORDER BY created_at DESC
	`
	rows, err := s.db.Query(query, filePath)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return s.scanChunks(rows)
}

func (s *SQLiteStore) GetRecentChunks(limit int) ([]chunk.Chunk, error) {
	query := `
		SELECT id, file_path, diff, start_time, end_time, hash, manual
		FROM chunks 
		ORDER BY created_at DESC
		LIMIT ?
	`
	rows, err := s.db.Query(query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return s.scanChunks(rows)
}

func (s *SQLiteStore) scanChunks(rows *sql.Rows) ([]chunk.Chunk, error) {
	var chunks []chunk.Chunk
	for rows.Next() {
		var c chunk.Chunk
		err := rows.Scan(&c.ID, &c.FilePath, &c.Diff, &c.StartTime, &c.EndTime, &c.Hash, &c.Manual)
		if err != nil {
			return nil, err
		}
		chunks = append(chunks, c)
	}
	return chunks, rows.Err()
}

func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

type JSONStore struct {
	filePath string
	chunks   []chunk.Chunk
}

func NewJSONStore(filePath string) *JSONStore {
	return &JSONStore{
		filePath: filePath,
		chunks:   make([]chunk.Chunk, 0),
	}
}

func (s *JSONStore) SaveChunk(c chunk.Chunk) error {
	for i, existing := range s.chunks {
		if existing.ID == c.ID {
			s.chunks[i] = c
			return s.persist()
		}
	}
	s.chunks = append(s.chunks, c)
	return s.persist()
}

func (s *JSONStore) FindChunks(filePath string) ([]chunk.Chunk, error) {
	var result []chunk.Chunk
	for _, c := range s.chunks {
		if c.FilePath == filePath {
			result = append(result, c)
		}
	}
	return result, nil
}

func (s *JSONStore) GetRecentChunks(limit int) ([]chunk.Chunk, error) {
	if limit > len(s.chunks) {
		limit = len(s.chunks)
	}
	result := make([]chunk.Chunk, limit)
	copy(result, s.chunks[len(s.chunks)-limit:])
	return result, nil
}

func (s *JSONStore) persist() error {
	return nil
}
